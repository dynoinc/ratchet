from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker, declarative_base
from sqlalchemy.exc import ProgrammingError
from app.config import Settings

Base = declarative_base()
SessionLocal = None  # Initialize as None
settings = Settings()
engine = create_engine(settings.database_url)


def init_db():
    """Initialize the database, creating tables if they don't exist."""
    global SessionLocal  # Declare SessionLocal as global
    try:
        Base.metadata.create_all(engine)
        print("Tables created successfully.")
    except ProgrammingError as e:
        print(f"An error occurred while creating tables: {e}")

    SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)

    return engine, SessionLocal


def get_db():
    if SessionLocal is None:
        raise RuntimeError("Database not initialized. Call init_db first.")
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()
