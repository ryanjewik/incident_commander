FROM python:3.11-slim

WORKDIR /app

ENV AGENT_WAIT_TIMEOUT_SECONDS="180"
ENV AGENT_POLL_INTERVAL_SECONDS="2"

COPY common ./common
COPY specialized_agents/moderator_agent ./agents/specialized_agents/moderator_agent

COPY specialized_agents/moderator_agent/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

ENV PYTHONPATH=/app

CMD ["python", "agents/specialized_agents/moderator_agent/main.py"]
