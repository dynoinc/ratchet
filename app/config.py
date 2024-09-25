from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    database_url: str
    slack_bot_token: str
    slack_app_token: str
    slack_channels: list[str]
    slack_messages_from_users: list[str]
    class Config:
        env_file = ".env"
