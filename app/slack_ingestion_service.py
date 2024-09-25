import asyncio
import logging
from slack_sdk.web.async_client import AsyncWebClient
from slack_sdk.errors import SlackApiError
from slack_bolt.async_app import AsyncApp
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from app.database import get_db
from app.models import Team, Activity, ActivityType, ActivityStatus, create_team, create_activity, get_team_by_slack_channel, update_activity, get_or_create_channel_status, update_channel_status
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
        logger.debug(f"Channel info: {channel_info}")
        channel_name = channel_info["channel"]["name"]

        # Store or update team information
        db_team = get_team_by_slack_channel(db, channel_id)
        if not db_team:
            db_team = create_team(db, channel_name, channel_id)

        # Get the last processed timestamp for this channel
        channel_status = get_or_create_channel_status(db, channel_id)
        latest_timestamp = channel_status.last_processed_timestamp

        # Fetch messages
        params = {"channel": channel_id}
        if latest_timestamp > 0:
            params["oldest"] = str(latest_timestamp)

        result = await client.conversations_history(**params)
        messages = result["messages"]
        logger.info(f"Fetched {len(messages)} messages from channel {channel_name}")

        newest_timestamp = latest_timestamp
        for msg in messages:
            logger.debug(f"Processing message: {msg}")
            
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
                        newest_timestamp = max(newest_timestamp, float(thread_msg["ts"]))

        # Update the last processed timestamp for this channel
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
                    # Continue with the next channel
                    continue
        except Exception as e:
            logger.error(f"Unexpected error in start_slack_ingestion: {str(e)}")
            logger.exception("Traceback:")
        finally:
            db.close()
        await asyncio.sleep(60)  # Wait for 60 seconds before the next ingestion cycle

@app.event("message")
async def handle_message_events(body, logger):
    db = next(get_db())
    try:
        logger.info(f"Handling message event: {body}")
        channel_id = body["event"]["channel"]
        db_team = get_team_by_slack_channel(db, channel_id)
        if not db_team:
            # Fetch channel info if it doesn't exist in our database
            client = AsyncWebClient(token=settings.slack_bot_token)
            channel_info = await client.conversations_info(channel=channel_id)
            channel_name = channel_info["channel"]["name"]
            db_team = create_team(db, channel_name, channel_id)

        msg = body["event"]
        
        if not should_we_persist_message(msg):
            return

        db_activity = determine_and_create_activity(db, db_team, msg)

        logger.info(f"Activity added to database: {db_activity.id}")
    except Exception as e:
        logger.error(f"Error handling message event: {e}")
        logger.exception("Traceback:")
    finally:
        db.close()

async def start_socket_mode():
    await validate_slack_tokens()
    handler = AsyncSocketModeHandler(app, settings.slack_app_token)
    await handler.start_async()
