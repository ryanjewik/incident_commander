FROM python:3.11-slim

# Copy entire agents context into the image under /app/agents
WORKDIR /app
COPY . ./agents
COPY ../common ./common

# Install dependencies from the agent requirements
RUN pip install --no-cache-dir -r agents/specialized_agents/rca_historical_agent/requirements.txt

# Set working directory to the agent and run
WORKDIR /app/agents/specialized_agents/rca_historical_agent
ENV PYTHONPATH=/app
CMD ["python", "main.py"]
