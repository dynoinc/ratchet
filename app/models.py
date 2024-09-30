import logging
from sqlalchemy import (
    Column,
    Integer,
    String,
    DateTime,
    ForeignKey,
    ARRAY,
    Text,
    Enum,
    Float,
)
from sqlalchemy.orm import relationship, backref
from sqlalchemy.sql import func
from app.database import Base, SessionLocal
import enum
from datetime import datetime
from sqlalchemy.dialects.postgresql import ARRAY as PG_ARRAY

# Set up logging
logger = logging.getLogger(__name__)


class ActivityType(enum.Enum):
    ALERT = "alert"
    BOT_MESSAGE = "bot_message"
    HUMAN_THREAD = "human_thread"


class ActivityStatus(enum.Enum):
    FIRED = "fired"
    DEBUGGING = "debugging"
    MITIGATED = "mitigated"
    ONGOING = "ongoing"
    RESOLVED = "resolved"


class Team(Base):
    __tablename__ = "teams"

    id = Column(Integer, primary_key=True, index=True)
    name = Column(String, unique=True, index=True)
    slack_channel_ids = Column(PG_ARRAY(String))  # Changed to PostgreSQL-specific ARRAY
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now()
    )
    channels = relationship("Channel", back_populates="team")
    activities = relationship("Activity", back_populates="team")


class Channel(Base):
    __tablename__ = "channels"

    id = Column(Integer, primary_key=True, index=True)
    slack_channel_id = Column(String, unique=True, index=True)
    name = Column(String)
    team_id = Column(Integer, ForeignKey("teams.id"))
    monitored_bot_accounts = Column(ARRAY(String))
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now()
    )
    team = relationship("Team", back_populates="channels")


class Activity(Base):
    __tablename__ = "activities"

    id = Column(Integer, primary_key=True, autoincrement=True)
    team_id = Column(Integer, ForeignKey("teams.id"), nullable=False)
    activity_type = Column(Enum(ActivityType), nullable=False)
    status = Column(Enum(ActivityStatus), nullable=False)
    content = Column(Text, nullable=False)
    timestamp = Column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    parent_activity_id = Column(Integer, ForeignKey("activities.id"), nullable=True)

    team = relationship("Team", back_populates="activities")
    child_activities = relationship(
        "Activity",
        backref=backref("parent", remote_side=[id]),
        cascade="all, delete-orphan",
    )


class ChannelProcessingStatus(Base):
    __tablename__ = "channel_processing_status"

    id = Column(Integer, primary_key=True, autoincrement=True)
    slack_channel_id = Column(String(255), unique=True, nullable=False)
    last_processed_timestamp = Column(Float, nullable=False, default=0)
    updated_at = Column(
        DateTime(timezone=True), server_default=func.now(), onupdate=func.now()
    )


def create_team(db: SessionLocal, name: str, slack_channel_id: str):
    new_team = Team(name=name, slack_channel_ids=[slack_channel_id])
    db.add(new_team)
    db.commit()
    db.refresh(new_team)
    logger.info(f"Created new team: {new_team.name} (ID: {new_team.id})")
    return new_team


def create_activity(
    db: SessionLocal,
    team_id: int,
    activity_type: ActivityType,
    status: ActivityStatus,
    content: str,
    timestamp: datetime = None,
    parent_activity_id: int = None,
):
    new_activity = Activity(
        team_id=team_id,
        activity_type=activity_type,
        status=status,
        content=content,
        timestamp=timestamp or datetime.utcnow(),
        parent_activity_id=parent_activity_id,
    )
    db.add(new_activity)
    db.commit()
    db.refresh(new_activity)
    logger.info(
        f"Created new activity: {new_activity.id} (Type: {activity_type}, Status: {status})"
    )
    return new_activity


def get_team(db: SessionLocal, team_id: int):
    return db.query(Team).filter(Team.id == team_id).first()


def get_team_by_slack_channel(db: SessionLocal, slack_channel_id: str):
    return db.query(Team).filter(Team.slack_channel_ids.any(slack_channel_id)).first()


def update_activity(
    db: SessionLocal,
    activity_id: int,
    new_status: ActivityStatus = None,
    new_content: str = None,
):
    activity = db.query(Activity).filter(Activity.id == activity_id).first()
    if activity:
        if new_status:
            activity.status = new_status
        if new_content:
            activity.content = new_content
        db.commit()
        return activity
    return None


def get_activities_by_team(db: SessionLocal, team_id: int):
    return db.query(Activity).filter(Activity.team_id == team_id).all()


def get_activities_by_type(db: SessionLocal, activity_type: ActivityType):
    return db.query(Activity).filter(Activity.activity_type == activity_type).all()


def get_or_create_channel_status(db: SessionLocal, slack_channel_id: str):
    channel_status = (
        db.query(ChannelProcessingStatus)
        .filter_by(slack_channel_id=slack_channel_id)
        .first()
    )
    if not channel_status:
        channel_status = ChannelProcessingStatus(slack_channel_id=slack_channel_id)
        db.add(channel_status)
        db.commit()
        db.refresh(channel_status)
        logger.info(f"Created new channel status for: {slack_channel_id}")
    return channel_status


def update_channel_status(
    db: SessionLocal, slack_channel_id: str, new_timestamp: float
):
    channel_status = get_or_create_channel_status(db, slack_channel_id)
    channel_status.last_processed_timestamp = new_timestamp
    db.commit()
    logger.info(
        f"Updated channel status for {slack_channel_id}: new timestamp {new_timestamp}"
    )
    return channel_status
