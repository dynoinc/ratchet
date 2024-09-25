import asyncio
import logging
from slack_sdk.web.async_client import AsyncWebClient
from slack_sdk.errors import SlackApiError
from slack_bolt.async_app import AsyncApp
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from app.database import get_db
from app.models import Activity, ActivityType, ActivityStatus, create_team, create_activity, get_team_by_slack_channel, get_or_create_channel_status, update_channel_status
from app.config import Settings
from datetime import datetime

settings = Settings()
app = AsyncApp(token=settings.slack_bot_token)

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

async def validate_slack_tokens():
    bot_client = AsyncWebClient(token=settings.slack_bot_token)
    app_client = AsyncWebClient(token=settings.slack_app_token)

    try:
        # Validate bot token
        bot_auth = await bot_client.auth_test()
        logger.info(f"Bot token validated. Connected as: {bot_auth['user']}")

        # Validate app token
        app_auth = await app_client.auth_test()
        logger.info(f"App token validated. Connected as: {app_auth['user']}")
    except SlackApiError as e:
        logger.error(f"Error validating Slack tokens: {e}")
        raise ValueError("Invalid Slack tokens. Please check your configuration.")

def should_we_persist_message(msg):
    return msg.get("bot_id") is not None and msg.get("bot_profile", {}).get("name") in settings.slack_messages_from_users

def determine_and_create_activity(db, db_team, msg, parent_activity_id=None):
    if "alert" in msg.get("text", "").lower():
        activity_type = ActivityType.ALERT
        status = ActivityStatus.FIRED
    elif msg.get("bot_id"):
        activity_type = ActivityType.BOT_MESSAGE
        status = ActivityStatus.ONGOING
    else:
        activity_type = ActivityType.HUMAN_THREAD
        status = ActivityStatus.ONGOING

    timestamp = datetime.fromtimestamp(float(msg["ts"]))

    db_activity = create_activity(
        db,
        db_team.id,
        activity_type,
        status,
        msg.get("text", ""),
        timestamp=timestamp,
        parent_activity_id=parent_activity_id
    )
    return db_activity

async def fetch_and_store_activities(
    client: AsyncWebClient, channel_id: str, db
):
    try:
        logger.info(f"Fetching activities for channel: {channel_id}")
        channel_info = await client.conversations_info(channel=channel_id)
        channel_name = channel_info["channel"]["name"]

        db_team = get_team_by_slack_channel(db, channel_id)
        if not db_team:
            db_team = create_team(db, channel_name, channel_id)

        channel_status = get_or_create_channel_status(db, channel_id)
        latest_timestamp = channel_status.last_processed_timestamp

        params = {"channel": channel_id}
        if latest_timestamp > 0:
            params["oldest"] = str(latest_timestamp)

        result = await client.conversations_history(**params)
        messages = result["messages"]
        logger.info(f"Fetched {len(messages)} messages from channel {channel_name}")

        newest_timestamp = latest_timestamp
        for msg in messages:
            current_msg_timestamp = float(msg["ts"])
            newest_timestamp = max(newest_timestamp, current_msg_timestamp)

            if should_we_persist_message(msg):
                db_activity = determine_and_create_activity(db, db_team, msg)

                if "thread_ts" in msg or msg.get("reply_count", 0) > 0:
                    thread_result = await client.conversations_replies(
                        channel=channel_id, ts=msg["ts"]
                    )
                    thread_messages = thread_result["messages"][1:]  # Exclude the parent message
                    for thread_msg in thread_messages:
                        thread_activity = determine_and_create_activity(db, db_team, thread_msg, db_activity.id)
                        logger.info(f"Thread activity added to database: {thread_activity.id}")
                        newest_timestamp = max(newest_timestamp, float(thread_msg["ts"]))

        update_channel_status(db, channel_id, newest_timestamp)
        logger.info(f"Updated channel {channel_id} last processed timestamp to {newest_timestamp}")

    except SlackApiError as e:
        logger.error(f"Error fetching activities: {e}")
        if "invalid_ts_oldest" in str(e):
            logger.warning("Invalid timestamp detected. Resetting channel status.")
            update_channel_status(db, channel_id, 0)

async def start_slack_ingestion():
    await validate_slack_tokens()
    client = AsyncWebClient(token=settings.slack_bot_token)

    while True:
        db = next(get_db())
        try:
            for channel in settings.slack_channels:
                logger.info(f"Processing channel: {channel}")
                try:
                    await fetch_and_store_activities(client, channel, db)
                except Exception as e:
                    logger.error(f"Error processing channel {channel}: {str(e)}")
                    logger.exception("Traceback:")
                    continue
        except Exception as e:
            logger.error(f"Unexpected error in start_slack_ingestion: {str(e)}")
            logger.exception("Traceback:")
        finally:
            db.close()
        await asyncio.sleep(60)  # Wait for 60 seconds before the next ingestion cycle

@app.event("message")
async def handle_message_events(body, logger):
    logger.info(f"Received message event: {body}")
    
    # Add more detailed logging
    event = body.get("event", {})
    channel_type = event.get("channel_type")
    logger.info(f"Message channel type: {channel_type}")
    
    db = next(get_db())
    try:
        channel_id = event.get("channel")
        logger.info(f"Channel ID: {channel_id}")
        if "thread_ts" in event and event["thread_ts"] != event["ts"]:
            logger.info("Processing as thread reply")
            await process_thread_reply(db, event)
        else:
            logger.info("Processing as main message")
            await process_main_message(db, event)

    except Exception as e:
        logger.error(f"Error handling message event: {e}")
        logger.exception("Traceback:")
    finally:
        db.close()

# Add a catch-all event handler for debugging
# @app.event("*")
# async def handle_all_events(body, logger):
#     logger.info(f"Received event: {body.get('event', {}).get('type')}")
#     logger.debug(f"Full event body: {body}")

async def process_main_message(db, event):
    channel_id = event["channel"]
    db_team = get_team_by_slack_channel(db, channel_id)
    if not db_team:
        client = AsyncWebClient(token=settings.slack_bot_token)
        channel_info = await client.conversations_info(channel=channel_id)
        channel_name = channel_info["channel"]["name"]
        db_team = create_team(db, channel_name, channel_id)

    if should_we_persist_message(event):
        db_activity = determine_and_create_activity(db, db_team, event)
        logger.info(f"Main activity added to database: {db_activity.id}")

async def process_thread_reply(db, event):
    channel_id = event["channel"]
    thread_ts = event["thread_ts"]
    
    db_team = get_team_by_slack_channel(db, channel_id)
    if not db_team:
        logger.error(f"Team not found for channel {channel_id}")
        return

    parent_activity = db.query(Activity).filter(
        Activity.team_id == db_team.id,
        Activity.timestamp == datetime.fromtimestamp(float(thread_ts))
    ).first()

    if parent_activity:
        thread_activity = determine_and_create_activity(db, db_team, event, parent_activity.id)
        logger.info(f"Thread reply activity added to database: {thread_activity.id}")
    else:
        logger.warning(f"Parent activity not found for thread_ts: {thread_ts}")

@app.event("app_mention")
async def handle_app_mentions(body, say):
    logger.info(f"Bot mentioned: {body}")
    await say("Hello! I'm listening to messages and processing them.")

async def start_socket_mode():
    handler = AsyncSocketModeHandler(app, settings.slack_app_token)
    logger.info("Starting Socket Mode handler")
    await handler.start_async()
    logger.info("Socket Mode handler started")

async def main():
    await validate_slack_tokens()
    ingestion_task = asyncio.create_task(start_slack_ingestion())
    socket_mode_task = asyncio.create_task(start_socket_mode())
    await asyncio.gather(ingestion_task, socket_mode_task)
