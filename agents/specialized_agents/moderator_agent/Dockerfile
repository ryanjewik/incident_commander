FROM python:3.11-slim

WORKDIR /app

COPY common ./common
COPY specialized_agents/metrics_agent ./agents/specialized_agents/metrics_agent

COPY specialized_agents/metrics_agent/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

ENV PYTHONPATH=/app

CMD ["python", "agents/specialized_agents/metrics_agent/main.py"]
