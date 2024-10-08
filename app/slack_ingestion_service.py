import asyncio
import logging
from slack_sdk.web.async_client import AsyncWebClient
from slack_sdk.errors import SlackApiError
from slack_bolt.async_app import AsyncApp
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from app.database import get_db
from app.models import (
    Channel,
    Team,
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
logging.basicConfig(level=logging.DEBUG)
logger = logging.getLogger(__name__)

# Update the onboarding_state structure
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
    # First, try to get the team by name
    team = db.query(Team).filter(Team.name == team_name).first()

    if team:
        # If the team exists, check if the channel is already associated
        if channel_id not in team.slack_channel_ids:
            team.slack_channel_ids.append(channel_id)
            db.commit()
    else:
        # If the team doesn't exist, try to get it by channel_id
        team = get_team_by_slack_channel(db, channel_id)

        if not team:
            # If still no team found, create a new one
            team = create_team(db, team_name, channel_id)
        elif channel_id not in team.slack_channel_ids:
            # If team exists but channel is not associated, add it
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
    while True:
        try:
            db = next(get_db())
            try:
                channels = db.query(Channel).all()
                for channel in channels:
                    logger.info(f"Processing channel: {channel.name}")
                    try:
                        client = AsyncWebClient(token=settings.slack_bot_token)
                        await fetch_and_store_activities(client, channel, db)
                    except Exception as e:
                        logger.error(
                            f"Error processing channel {channel.name}: {str(e)}"
                        )
                        logger.exception("Traceback:")
                        continue
            except Exception as e:
                logger.error(f"Unexpected error in start_slack_ingestion: {str(e)}")
                logger.exception("Traceback:")
            finally:
                db.close()
            await asyncio.sleep(
                60
            )  # Wait for 60 seconds before the next ingestion cycle
        except Exception as e:
            logger.error(f"Critical error in start_slack_ingestion: {str(e)}")
            logger.exception("Traceback:")
            await asyncio.sleep(
                300
            )  # Wait for 5 minutes before retrying after a critical error


@app.event("app_mention")
async def handle_app_mentions(body, say, client):
    event = body["event"]
    channel_id = event["channel"]
    text = event["text"].lower()
    ts = event["ts"]
    thread_ts = event.get(
        "thread_ts", ts
    )  # Use thread_ts if available, otherwise use ts

    logger.debug(f"Received app mention in channel {channel_id}: {text}")

    db = next(get_db())
    try:
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )
        if channel:
            await client.chat_postMessage(
                channel=channel_id,
                text="Hey there! 👋 I'm like a toddler right now - full of potential, but still learning to walk and talk. 🚶‍♂️🗣️ I can't be much help at the moment, but I'm growing fast! Soon, I'll be your go-to AI assistant. For now, let's just enjoy this awkward silence together, shall we? 😅",
                thread_ts=thread_ts,
            )
            logger.debug(
                f"Bot already set up in channel {channel_id}, sent friendly message"
            )
        else:
            onboarding_state[channel_id] = {
                "step": "waiting_for_assist",
                "thread_ts": thread_ts,
            }
            await client.chat_postMessage(
                channel=channel_id,
                text="I'm not set up for this channel yet. Please say 'assist' to start the setup process.",
                thread_ts=thread_ts,
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
    text = event.get("text", "").strip().lower()
    thread_ts = event.get("thread_ts")

    logger.debug(f"Received message in channel {channel_id}: {text}")

    db = next(get_db())
    try:
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )

        if channel_id in onboarding_state:
            state = onboarding_state[channel_id]
            if (
                state["step"] == "waiting_for_assist"
                and thread_ts == state["thread_ts"]
                and "assist" in text
            ):
                state["step"] = "team_name"
                await client.chat_postMessage(
                    channel=channel_id,
                    text="Great! Let's set up the bot for this channel. Please provide a team name.",
                    thread_ts=thread_ts,
                )
                logger.debug(f"Started onboarding for channel {channel_id}")
            elif thread_ts == state["thread_ts"]:
                await handle_onboarding(channel_id, text, client, db)
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


async def handle_onboarding(channel_id, text, client, db):
    state = onboarding_state[channel_id]
    thread_ts = state["thread_ts"]

    if state["step"] == "team_name":
        team = await get_or_create_team(db, text, channel_id)
        state["team_id"] = team.id

        # Get channel info
        channel_info = await client.conversations_info(channel=channel_id)
        channel_name = channel_info["channel"]["name"]

        # Create or get channel
        channel = await get_or_create_channel(db, channel_id, channel_name, team.id)

        state["step"] = "bot_accounts"

        # Check if this is a new team or an existing one
        if channel_id in team.slack_channel_ids and len(team.slack_channel_ids) > 1:
            message = f"Team '{text}' already exists. This channel #{channel_name} has been added to the team. Now, please provide a comma-separated list of bot accounts to monitor in this channel."
        else:
            message = f"Team '{text}' registered for channel #{channel_name}. Now, please provide a comma-separated list of bot accounts to monitor in this channel."

        await client.chat_postMessage(
            channel=channel_id, text=message, thread_ts=thread_ts
        )
        logger.debug(f"Team name set for channel {channel_id}: {text}")

    elif state["step"] == "bot_accounts":
        bot_accounts = [account.strip() for account in text.split(",")]
        channel = (
            db.query(Channel).filter(Channel.slack_channel_id == channel_id).first()
        )
        channel.monitored_bot_accounts = bot_accounts
        db.commit()
        await client.chat_postMessage(
            channel=channel_id,
            text=f"Thank you! I'll monitor the following bot accounts: {', '.join(bot_accounts)}. Setup is complete!",
            thread_ts=thread_ts,
        )
        logger.debug(
            f"Onboarding completed for channel {channel_id}. Monitored bots: {bot_accounts}"
        )
        del onboarding_state[channel_id]


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
    while True:
        try:
            handler = AsyncSocketModeHandler(app, settings.slack_app_token)
            logger.info("Starting Socket Mode handler")
            await handler.start_async()
            logger.info("Socket Mode handler started")
        except Exception as e:
            logger.error(f"Error in Socket Mode handler: {str(e)}")
            logger.exception("Traceback:")
            await asyncio.sleep(300)  # Wait for 5 minutes before retrying
