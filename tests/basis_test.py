import pytest
from app.main import app
from fastapi.testclient import TestClient


@pytest.mark.asyncio
async def test_root() -> None:
    client = TestClient(app)
    response = client.get("/")

    assert response.status_code == 200
    assert response.json() == {"message": "Hello World"}
