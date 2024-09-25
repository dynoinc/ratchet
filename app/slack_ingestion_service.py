import asyncio
from slack_sdk.web.async_client import AsyncWebClient
from slack_sdk.errors import SlackApiError
from slack_bolt.async_app import AsyncApp
from slack_bolt.adapter.socket_mode.async_handler import AsyncSocketModeHandler
from app.database import get_db
from app.models import SlackChannel, SlackMessage, ThreadMessage
from app.config import Settings
from datetime import datetime

settings = Settings()
app = AsyncApp(token=settings.slack_bot_token)


async def validate_slack_tokens():
    bot_client = AsyncWebClient(token=settings.slack_bot_token)
    app_client = AsyncWebClient(token=settings.slack_app_token)

    try:
        # Validate bot token
        bot_auth = await bot_client.auth_test()
        print(f"Bot token validated. Connected as: {bot_auth['user']}")

        # Validate app token
        app_auth = await app_client.auth_test()
        print(f"App token validated. Connected as: {app_auth['user']}")
    except SlackApiError as e:
        print(f"Error validating Slack tokens: {e}")
        raise ValueError("Invalid Slack tokens. Please check your configuration.")


async def fetch_and_store_messages(
    client: AsyncWebClient, channel_id: str, db, latest_timestamp=None
):
    try:
        print(f"Fetching messages for channel: {channel_id}")
        # Fetch channel info
        channel_info = await client.conversations_info(channel=channel_id)
        print(f"Channel info: {channel_info}")
        channel_name = channel_info["channel"]["name"]

        # Store or update channel information
        db_channel = db.query(SlackChannel).filter_by(channel_id=channel_id).first()
        if not db_channel:
            db_channel = SlackChannel(name=channel_name, channel_id=channel_id)
            db.add(db_channel)
        else:
            db_channel.name = channel_name
        db.commit()

        # Fetch messages
        params = {"channel": channel_id}
        if latest_timestamp:
            params["oldest"] = latest_timestamp

        result = await client.conversations_history(**params)
        messages = result["messages"]
        print(f"Fetched {len(messages)} messages from channel {channel_name}")

        for msg in messages:
            timestamp = datetime.fromtimestamp(float(msg["ts"]))
            db_message = SlackMessage(
                channel_id=db_channel.id,
                user_id=msg.get("user", ""),
                content=msg.get("text", ""),
                timestamp=timestamp,
                has_thread="thread_ts" in msg or msg.get("reply_count", 0) > 0,
            )
            db.add(db_message)
            db.commit()

            if db_message.has_thread:
                thread_result = await client.conversations_replies(
                    channel=channel_id, ts=msg["ts"]
                )
                thread_messages = thread_result["messages"][
                    1:
                ]  # Exclude the parent message
                for thread_msg in thread_messages:
                    thread_timestamp = datetime.fromtimestamp(float(thread_msg["ts"]))
                    db_thread_message = ThreadMessage(
                        parent_message_id=db_message.id,
                        user_id=thread_msg.get("user", ""),
                        content=thread_msg.get("text", ""),
                        timestamp=thread_timestamp,
                    )
                    db.add(db_thread_message)
                db.commit()

    except SlackApiError as e:
        print(f"Error fetching messages: {e}")


async def start_slack_ingestion():
    await validate_slack_tokens()
    client = AsyncWebClient(token=settings.slack_bot_token)

    while True:
        db = next(get_db())
        try:
            for channel in settings.slack_channels:
                latest_message = (
                    db.query(SlackMessage)
                    .join(SlackChannel)
                    .filter(SlackChannel.channel_id == channel)
                    .order_by(SlackMessage.timestamp.desc())
                    .first()
                )
                latest_timestamp = (
                    latest_message.timestamp.timestamp() if latest_message else None
                )

                await fetch_and_store_messages(client, channel, db, latest_timestamp)
        except Exception as e:
            print(f"Error in start_slack_ingestion: {e}")
        finally:
            db.close()
        await asyncio.sleep(60)  # Wait for 60 seconds before the next ingestion cycle


@app.event("message")
async def handle_message_events(body, logger):
    db = next(get_db())
    try:
        channel_id = body["event"]["channel"]
        db_channel = db.query(SlackChannel).filter_by(channel_id=channel_id).first()
        if not db_channel:
            # Fetch channel info if it doesn't exist in our database
            client = AsyncWebClient(token=settings.slack_bot_token)
            channel_info = await client.conversations_info(channel=channel_id)
            channel_name = channel_info["channel"]["name"]
            db_channel = SlackChannel(name=channel_name, channel_id=channel_id)
            db.add(db_channel)
            db.commit()

        msg = body["event"]
        timestamp = datetime.fromtimestamp(float(msg["ts"]))
        db_message = SlackMessage(
            channel_id=db_channel.id,
            user_id=msg.get("user", ""),
            content=msg.get("text", ""),
            timestamp=timestamp,
            has_thread="thread_ts" in msg,
        )
        db.add(db_message)
        db.commit()

        logger.info(f"Message added to database: {db_message.id}")
    except Exception as e:
        logger.error(f"Error handling message event: {e}")
    finally:
        db.close()


async def start_socket_mode():
    await validate_slack_tokens()
    handler = AsyncSocketModeHandler(app, settings.slack_app_token)
    await handler.start_async()


if __name__ == "__main__":
    asyncio.run(start_socket_mode())
