from pydantic_settings import BaseSettings
from urllib.parse import quote_plus


class Settings(BaseSettings):
    database_type: str
    database_host: str = ""
    database_port: str = ""
    database_name: str
    database_user: str = ""
    database_password: str = ""
    slack_bot_token: str
    slack_app_token: str
    openai_api_key: str

    class Config:
        env_file = ".env"

    @property
    def database_url(self):
        if self.database_type == "sqlite":
            return f"sqlite:///{self.database_name}"
        elif self.database_type == "postgresql":
            password = quote_plus(self.database_password)
            return f"postgresql://{self.database_user}:{password}@{self.database_host}:{self.database_port}/{self.database_name}"
        else:
            raise ValueError(f"Unsupported database type: {self.database_type}")
