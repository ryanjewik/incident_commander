FROM python:3.11-slim

WORKDIR /app
COPY . ./agents
COPY common ./common

RUN pip install --no-cache-dir -r agents/specialized_agents/metrics_agent/requirements.txt

WORKDIR /app/agents/specialized_agents/metrics_agent
ENV PYTHONPATH=/app
ENV PYTHONUNBUFFERED=1
CMD ["python", "-u", "main.py"]