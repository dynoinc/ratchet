import asyncio
import logging
from slack_sdk.web.async_client import AsyncWebClient
from slack_sdk.errors import SlackApiError
from slack_bolt.async_app import AsyncApp
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from app.database import get_db
from app.models import (
    Channel,
    ActivityType,
    ActivityStatus,
    create_activity,
    get_or_create_channel_status,
    update_channel_status,
    get_team_by_slack_channel,
    create_team,
)
from app.config import Settings
from datetime import datetime

settings = Settings()
app = AsyncApp(token=settings.slack_bot_token)

# Set up logging
logging.basicConfig(level=logging.DEBUG)  # Changed to DEBUG for more detailed logs
logger = logging.getLogger(__name__)

# Dictionary to store onboarding state for each channel
onboarding_state = {}


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


async def get_or_create_team(db, team_name, channel_id):
    team = get_team_by_slack_channel(db, channel_id)
    if not team:
        team = create_team(db, team_name, channel_id)
    elif channel_id not in team.slack_channel_ids:
        team.slack_channel_ids.append(channel_id)
        db.commit()
    return team


async def get_or_create_channel(db, slack_channel_id, channel_name, team_id):
    channel = (
        db.query(Channel).filter(Channel.slack_channel_id == slack_channel_id).first()
    )
    if not channel:
        channel = Channel(
            slack_channel_id=slack_channel_id, name=channel_name, team_id=team_id
        )
        db.add(channel)
        db.commit()
    return channel


def should_we_persist_message(msg, monitored_bot_accounts):
    return (
        msg.get("bot_id") is not None
        and msg.get("bot_profile", {}).get("name") in monitored_bot_accounts
    )


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
        parent_activity_id=parent_activity_id,
    )
    return db_activity


async def fetch_and_store_activities(client: AsyncWebClient, channel: Channel, db):
    try:
        logger.info(f"Fetching activities for channel: {channel.slack_channel_id}")

        channel_status = get_or_create_channel_status(db, channel.slack_channel_id)
        latest_timestamp = channel_status.last_processed_timestamp

        params = {"channel": channel.slack_channel_id}
        if latest_timestamp > 0:
            params["oldest"] = str(latest_timestamp)

        result = await client.conversations_history(**params)
        messages = result["messages"]
        logger.info(f"Fetched {len(messages)} messages from channel {channel.name}")

        newest_timestamp = latest_timestamp
        for msg in messages:
            current_msg_timestamp = float(msg["ts"])
            newest_timestamp = max(newest_timestamp, current_msg_timestamp)

            if should_we_persist_message(msg, channel.monitored_bot_accounts):
                db_activity = determine_and_create_activity(db, channel.team, msg)

                if "thread_ts" in msg or msg.get("reply_count", 0) > 0:
                    thread_result = await client.conversations_replies(
                        channel=channel.slack_channel_id, ts=msg["ts"]
                    )
                    thread_messages = thread_result["messages"][
                        1:
                    ]  # Exclude the parent message
                    for thread_msg in thread_messages:
                        thread_activity = determine_and_create_activity(
                            db, channel.team, thread_msg, db_activity.id
                        )
                        logger.info(
                            f"Thread activity added to database: {thread_activity.id}"
                        )
                        newest_timestamp = max(
                            newest_timestamp, float(thread_msg["ts"])
                        )

        update_channel_status(db, channel.slack_channel_id, newest_timestamp)
        logger.info(
            f"Updated channel {channel.slack_channel_id} last processed timestamp to {newest_timestamp}"
        )

    except SlackApiError as e:
        logger.error(f"Error fetching activities: {e}")
        if "invalid_ts_oldest" in str(e):
            logger.warning("Invalid timestamp detected. Resetting channel status.")
            update_channel_status(db, channel.slack_channel_id, 0)


async def start_slack_ingestion():
    await validate_slack_tokens()
    client = AsyncWebClient(token=settings.slack_bot_token)

    while True:
        db = next(get_db())
        try:
            channels = db.query(Channel).all()
            for channel in channels:
                logger.info(f"Processing channel: {channel.name}")
                try:
                    await fetch_and_store_activities(client, channel, db)
                except Exception as e:
                    logger.error(f"Error processing channel {channel.name}: {str(e)}")
                    logger.exception("Traceback:")
                    continue
        except Exception as e:
            logger.error(f"Unexpected error in start_slack_ingestion: {str(e)}")
            logger.exception("Traceback:")
        finally:
            db.close()
        await asyncio.sleep(60)  # Wait for 60 seconds before the next ingestion cycle


@app.event("app_mention")
async def handle_app_mentions(body, say, client):
    event = body["event"]
    channel_id = event["channel"]
    text = event["text"].lower()

    logger.debug(f"Received app mention in channel {channel_id}: {text}")

    db = next(get_db())
    try:
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )
        if channel:
            await say("I'm already assisting in this channel. How can I help you?")
            logger.debug(f"Bot already set up in channel {channel_id}")
        else:
            if "assist" in text:
                onboarding_state[channel_id] = {"step": "team_name"}
                await say(
                    "Great! Let's set up the bot for this channel. Please provide a team name."
                )
                logger.debug(f"Started onboarding for channel {channel_id}")
            else:
                await say(
                    "I'm not set up for this channel yet. Please say 'assist' to start the setup process."
                )
                logger.debug(
                    f"Bot not set up in channel {channel_id}, waiting for 'assist' command"
                )
    finally:
        db.close()


@app.event("message")
async def handle_messages(body, say, client):
    event = body["event"]
    channel_id = event["channel"]
    text = event.get("text", "").strip()

    logger.debug(f"Received message in channel {channel_id}: {text}")

    db = next(get_db())
    try:
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )

        if channel_id in onboarding_state:
            await handle_onboarding(channel_id, text, say, db)
        elif channel and should_we_persist_message(
            event, channel.monitored_bot_accounts
        ):
            determine_and_create_activity(db, channel.team, event)
            logger.info(f"Activity added to database for channel: {channel.name}")
        else:
            logger.debug(
                "Message not processed: channel not set up or message not from monitored bot"
            )

    finally:
        db.close()


async def handle_onboarding(channel_id, text, say, db):
    state = onboarding_state[channel_id]

    if state["step"] == "team_name":
        team = await get_or_create_team(db, text, channel_id)
        state["team_id"] = team.id
        state["step"] = "channel_name"
        await say(
            f"Team '{text}' registered. Now, please provide a name for this channel."
        )
        logger.debug(f"Team name set for channel {channel_id}: {text}")

    elif state["step"] == "channel_name":
        channel = await get_or_create_channel(db, channel_id, text, state["team_id"])
        state["step"] = "bot_accounts"
        await say(
            "Channel name set. Finally, please provide a comma-separated list of bot accounts to monitor in this channel."
        )
        logger.debug(f"Channel name set for channel {channel_id}: {text}")

    elif state["step"] == "bot_accounts":
        bot_accounts = [account.strip() for account in text.split(",")]
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )
        channel.monitored_bot_accounts = bot_accounts
        db.commit()
        del onboarding_state[channel_id]
        await say(
            f"Thank you! I'll monitor the following bot accounts: {', '.join(bot_accounts)}. Setup is complete!"
        )
        logger.debug(
            f"Onboarding completed for channel {channel_id}. Monitored bots: {bot_accounts}"
        )


@app.event("member_joined_channel")
async def handle_member_joined(body, say, client):
    event = body["event"]
    channel_id = event["channel"]
    user_id = event["user"]

    logger.debug(f"Member joined channel {channel_id}: {user_id}")

    bot_info = await client.auth_test()
    if user_id == bot_info["user_id"]:
        await say(
            "Hello! I've been added to this channel. To set me up, please mention me and say 'assist'."
        )
        logger.debug(f"Bot joined channel {channel_id}")


async def setup_channel(channel_id, team_name, bot_accounts, db):
    client = AsyncWebClient(token=settings.slack_bot_token)
    channel_info = await client.conversations_info(channel=channel_id)
    channel_name = channel_info["channel"]["name"]

    team = await get_or_create_team(db, team_name, channel_id)
    channel = await get_or_create_channel(db, channel_id, channel_name, team.id)
    channel.monitored_bot_accounts = bot_accounts
    db.commit()

    logger.info(f"Channel {channel_name} set up for team {team_name}")


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
