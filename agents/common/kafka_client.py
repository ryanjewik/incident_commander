# common/kafka_client.py
from __future__ import annotations
from confluent_kafka import Consumer, Producer
from confluent_kafka.admin import AdminClient, NewTopic
from pathlib import Path
from typing import Dict, Optional

def _parse_properties_file(path: str) -> Dict[str, str]:
    props: Dict[str, str] = {}
    p = Path(path)
    for raw in p.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            continue
        k, v = line.split("=", 1)
        props[k.strip()] = v.strip()
    return props

def build_kafka_config(client_properties_path: str, env: Dict[str, str]) -> Dict[str, str]:
    """
    Reads client.properties then overrides secrets via env:
      - CONFLUENT_API_KEY -> sasl.username
      - CONFLUENT_SECRET -> sasl.password
    """
    cfg = _parse_properties_file(client_properties_path)
    if env.get("CONFLUENT_API_KEY"):
        cfg["sasl.username"] = env["CONFLUENT_API_KEY"]
    # Use the canonical environment variable name for the secret.
    if env.get("CONFLUENT_SECRET"):
        cfg["sasl.password"] = env["CONFLUENT_SECRET"]
    # Ensure consumer-only settings are not present on the shared config
    for _k in ("group.id", "auto.offset.reset", "enable.auto.commit", "session.timeout.ms"):
        cfg.pop(_k, None)
    return cfg

def make_consumer(
    cfg: Dict[str, str],
    group_id: str,
    *,
    auto_offset_reset: str = "earliest",
    enable_auto_commit: bool = True,
) -> Consumer:
    return Consumer(
        {
            **cfg,
            "group.id": group_id,
            "auto.offset.reset": auto_offset_reset,
            "enable.auto.commit": enable_auto_commit,
        }
    )

def make_producer(cfg: Dict[str, str]) -> Producer:
    # Remove consumer-only settings that may cause librdkafka warnings when used by a Producer
    producer_cfg = dict(cfg)
    for k in ("group.id", "auto.offset.reset", "enable.auto.commit", "session.timeout.ms"):
        producer_cfg.pop(k, None)
    return Producer(producer_cfg)

def ensure_topics(
    cfg: Dict[str, str],
    topics: list[str],
    num_partitions: int = 1,
    replication_factor: int = 1,
    timeout: int = 10,
) -> None:
    """Create topics if they do not already exist. Silently continues on errors."""
    try:
        admin = AdminClient(cfg)
        new_topics = [
            NewTopic(topic, num_partitions=num_partitions, replication_factor=replication_factor)
            for topic in topics
        ]
        fs = admin.create_topics(new_topics, request_timeout=timeout)

        retry_with_rf3 = []
        for topic, f in fs.items():
            try:
                f.result(timeout=timeout)
            except Exception as e:
                msg = str(e)
                print(f"[kafka_client] topic create result for {topic}: {msg}")
                if "replication factor" in msg:
                    retry_with_rf3.append(topic)

        if retry_with_rf3 and replication_factor != 3:
            try:
                new_topics = [
                    NewTopic(topic, num_partitions=num_partitions, replication_factor=3)
                    for topic in retry_with_rf3
                ]
                fs2 = admin.create_topics(new_topics, request_timeout=timeout)
                for topic, f in fs2.items():
                    try:
                        f.result(timeout=timeout)
                    except Exception as e:
                        print(f"[kafka_client] retry topic create result for {topic}: {e}")
            except Exception as e:
                print(f"[kafka_client] retry ensure_topics failed: {e}")
    except Exception as e:
        print(f"[kafka_client] ensure_topics failed: {e}")
