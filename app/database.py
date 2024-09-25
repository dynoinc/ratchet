from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker, declarative_base
from sqlalchemy.exc import ProgrammingError

Base = declarative_base()
SessionLocal = None  # Initialize as None

def init_db(db_url: str):
    """Initialize the database, creating tables if they don't exist."""
    global SessionLocal  # Declare SessionLocal as global
    engine = create_engine(db_url)
    
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

# Import your models here
from app.models import SlackChannel, SlackMessage, ThreadMessage