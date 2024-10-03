# Stage 1: Build the application
FROM python:3.12-slim AS builder

# Install uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /bin/uv

# Copy the application into the container
COPY . /app
WORKDIR /app

# Install the application dependencies
RUN uv sync --frozen --no-cache

# Stage 3: Final stage
FROM python:3.12-slim

# Copy uv from the builder stage
COPY --from=builder /bin/uv /bin/uv

# Copy the application and installed packages from the builder stage
COPY --from=builder /app/app /app
COPY --from=builder /app/.venv /app/.venv
COPY --from=builder /usr/local/lib/python*/site-packages /usr/local/lib/python*/site-packages
WORKDIR /

# Run the application
CMD ["/app/.venv/bin/uvicorn", "app.main:app", "--port", "5001", "--host", "127.0.0.1"]
