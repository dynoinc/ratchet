from sqlalchemy import Column, Integer, String, Text, Boolean, DateTime, ForeignKey
from sqlalchemy.orm import relationship
from sqlalchemy.sql import func
from app.database import Base

class SlackChannel(Base):
    __tablename__ = 'slack_channels'

    id = Column(Integer, primary_key=True, autoincrement=True)
    name = Column(String(255), nullable=False)
    channel_id = Column(String(255), unique=True, nullable=False)
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())

    messages = relationship("SlackMessage", back_populates="channel")

class SlackMessage(Base):
    __tablename__ = 'slack_messages'

    id = Column(Integer, primary_key=True, autoincrement=True)
    channel_id = Column(Integer, ForeignKey('slack_channels.id'), nullable=False)
    user_id = Column(String(255), nullable=False)
    content = Column(Text, nullable=False)
    timestamp = Column(DateTime(timezone=True), nullable=False)
    has_thread = Column(Boolean, default=False)

    channel = relationship("SlackChannel", back_populates="messages")
    thread_messages = relationship("ThreadMessage", back_populates="parent_message")

class ThreadMessage(Base):
    __tablename__ = 'thread_messages'

    id = Column(Integer, primary_key=True, autoincrement=True)
    parent_message_id = Column(Integer, ForeignKey('slack_messages.id'), nullable=False)
    user_id = Column(String(255), nullable=False)
    content = Column(Text)
    image_url = Column(String(1024))
    timestamp = Column(DateTime(timezone=True), nullable=False)

    parent_message = relationship("SlackMessage", back_populates="thread_messages")
