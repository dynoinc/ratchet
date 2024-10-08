from fastapi import FastAPI, Depends
from sqlalchemy.orm import Session
from app.database import init_db, get_db
from app.config import Settings
from app.slack_ingestion_service import (
    start_slack_ingestion,
    validate_slack_tokens,
    start_socket_mode,
)
from contextlib import asynccontextmanager
import asyncio
import logging

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Validate Slack tokens
    try:
        await validate_slack_tokens()
        # Start Slack ingestion service and socket mode handler in the background
        slack_ingestion_task = asyncio.create_task(start_slack_ingestion())
        socket_mode_task = asyncio.create_task(start_socket_mode())
    except ValueError as e:
        logger.error(f"Error during Slack token validation: {e}")
        logger.warning("Slack integration will not be available.")
        slack_ingestion_task = None
        socket_mode_task = None

    yield

    # Cancel the tasks on shutdown if they were started
    if slack_ingestion_task:
        slack_ingestion_task.cancel()
    if socket_mode_task:
        socket_mode_task.cancel()

    if slack_ingestion_task or socket_mode_task:
        try:
            await asyncio.wait(
                [task for task in [slack_ingestion_task, socket_mode_task] if task],
                timeout=5.0,
            )
        except asyncio.TimeoutError:
            logger.warning("Graceful shutdown of Slack tasks timed out, forcing exit")
        except Exception as e:
            logger.error(f"Error during shutdown of Slack tasks: {e}")


app = FastAPI(lifespan=lifespan)
settings = Settings()

# Initialize the database
_ = init_db()


@app.get("/")
async def root(db: Session = Depends(get_db)) -> dict[str, str]:
    return {"message": "Hello World"}
