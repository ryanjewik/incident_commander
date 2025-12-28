from __future__ import annotations
from confluent_kafka import Consumer, Producer
from confluent_kafka.admin import AdminClient, NewTopic
from pathlib import Path
from typing import Dict


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
      - CONFLUENT_SECERET (typo) -> sasl.password
    """
    cfg = _parse_properties_file(client_properties_path)

    # Override with env (preferred)
    if env.get("CONFLUENT_API_KEY"):
        cfg["sasl.username"] = env["CONFLUENT_API_KEY"]
    if env.get("CONFLUENT_SECERET"):
        cfg["sasl.password"] = env["CONFLUENT_SECERET"]

    return cfg


def make_consumer(cfg: Dict[str, str], group_id: str) -> Consumer:
    c = Consumer(
        {
            **cfg,
            "group.id": group_id,
            "auto.offset.reset": "earliest",
            "enable.auto.commit": True,
	
        }
    )
    return c


def make_producer(cfg: Dict[str, str]) -> Producer:
    return Producer(cfg)


def ensure_topics(cfg: Dict[str, str], topics: list[str], num_partitions: int = 1, replication_factor: int = 1, timeout: int = 10) -> None:
    """Create topics if they do not already exist. Silently continues on errors.

    Args:
        cfg: Kafka client config (must include `bootstrap.servers`).
        topics: list of topic names to ensure exist.
    """
    try:
        admin = AdminClient(cfg)
        # Prepare list of NewTopic
        new_topics = [NewTopic(topic, num_partitions=num_partitions, replication_factor=replication_factor) for topic in topics]
        fs = admin.create_topics(new_topics, request_timeout=timeout)
        # Wait for results (log failures) and collect topics that failed due to replication policy
        retry_with_rf3 = []
        for topic, f in fs.items():
            try:
                f.result(timeout=timeout)
            except Exception as e:
                msg = str(e)
                print(f"[kafka_client] topic create result for {topic}: {msg}")
                if "replication factor must be 3" in msg or "replication factor" in msg:
                    retry_with_rf3.append(topic)

        # If the broker requires replication factor 3, retry creating those topics with rf=3
        if retry_with_rf3 and replication_factor != 3:
            try:
                new_topics = [NewTopic(topic, num_partitions=num_partitions, replication_factor=3) for topic in retry_with_rf3]
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
