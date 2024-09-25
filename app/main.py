from fastapi import FastAPI, Depends
from sqlalchemy.orm import Session
from app.database import init_db, get_db
from app.config import Settings
from app.slack_ingestion_service import (
    start_slack_ingestion,
    start_socket_mode,
    validate_slack_tokens,
)
from contextlib import asynccontextmanager
import asyncio


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Validate Slack tokens
    await validate_slack_tokens()

    # Start Slack ingestion service and socket mode handler
    slack_ingestion_task = asyncio.create_task(start_slack_ingestion())
    socket_mode_task = asyncio.create_task(start_socket_mode())
    yield
    # Cancel the tasks on shutdown
    slack_ingestion_task.cancel()
    socket_mode_task.cancel()
    try:
        await asyncio.wait_for(
            asyncio.gather(slack_ingestion_task, socket_mode_task), timeout=5.0
        )
    except asyncio.TimeoutError:
        print("Graceful shutdown timed out, forcing exit")
    except Exception as e:
        print(f"Error during shutdown: {e}")


app = FastAPI(lifespan=lifespan)
settings = Settings()

# Initialize the database
engine, SessionLocal = init_db(settings.database_url)


@app.get("/")
async def root(db: Session = Depends(get_db)) -> dict[str, str]:
    return {"message": "Hello World"}
