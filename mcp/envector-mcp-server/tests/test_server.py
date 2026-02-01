# tests/test_server.py
import os
import sys
import pytest

from typing import Union, List, Any, Dict, Optional

import numpy as np

# Add srcs directory to import path relative to project root
ROOT = os.path.dirname(os.path.dirname(__file__))
SRCS = os.path.join(ROOT, "srcs")
if SRCS not in sys.path:
    sys.path.append(SRCS)

from fastmcp import Client
from fastmcp.exceptions import ToolError
from server import MCPServerApp
from adapter import EnVectorSDKAdapter, EmbeddingAdapter

# embedding fake adapter
class FakeEmbeddingAdapter(EmbeddingAdapter):
    def __init__(self):
        pass  # Actual initialization not needed

    # ----------- Mocked method: get_embedding ----------- #
    def get_embedding(self, texts: List[str]) -> np.ndarray:
        # Return a fake response
        #   - Expected Return Type: List[Dict[str, Any]]
        return np.array([[0.1, 0.2, 0.3] * (i+1) for i in range(len(texts))])

@pytest.fixture
def mcp_server():
    """
    Create and return a FastMCP server instance for testing.
    Inject a fake adapter to avoid using the actual enVector SDK.
    """
    class FakeAdapter(EnVectorSDKAdapter):
        def __init__(self):
            pass  # Actual initialization not needed

        # ----------- Mocked method: Get Index List ----------- #
        def invoke_get_index_list(self) -> List[str]:
            return ["index_a", "index_b"]

        # ----------- Mocked method: Get Index Info ----------- #
        def invoke_get_index_info(self, index_name: str) -> Dict[str, Any]:
            if index_name not in ("index_a", "index_b"):
                raise ValueError(f"Index '{index_name}' not found")
            return {"index_name": index_name, "dim": 128, "row_count": 42}

        # ----------- Mocked method: Create Index ----------- #
        def invoke_create_index(self, index_name: str, dim: int, index_params: Dict[str, Any] = None) -> Dict[str, Any]:
            if index_params is not None and not isinstance(index_params, dict):
                raise TypeError("index_params must be a dict or None")
            return {"index_name": index_name, "dim": dim, "index_params": index_params}

        # ----------- Mocked method: Insert ----------- #
        def invoke_insert(
                self,
                index_name: str,
                vectors: List[List[float]],
                metadata: Union[Any, List[Any]] = None
            ) -> Dict[str, Any]:
            return {"index_name": index_name, "vectors": vectors, "metadata": metadata}

        # ----------- Mocked method: Search ----------- #
        def invoke_search(self, index_name: str, query: Union[List[float], List[List[float]]], topk: int) -> List[Dict[str, Any]]:
            # Return a fake response
            #   - Expected Return Type: List[Dict[str, Any]]
            return [{"id": 1, "score": 0.9, "metadata": {"fieldA": "valueA"}}]

    app = MCPServerApp(envector_adapter=FakeAdapter(), mcp_server_name="test-mcp", embedding_adapter=FakeEmbeddingAdapter())
    return app.mcp  # FastMCP Instance


# ----------- Create Index Tool Tests ----------- #
# Test cases for the 'create_index' tool in the MCP server
@pytest.mark.asyncio
async def test_tools_list_contains_create_index(mcp_server):
    async with Client(mcp_server) as client:
        tools = await client.list_tools()
        names = [t.name for t in tools]
        assert "create_index" in names


@pytest.mark.asyncio
async def test_call_tool_create_index_happy_path(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool(
            "create_index",
            {
                "index_name": "test_index",
                "dim": 128,
                "index_params": {"index_type": "FLAT"}
            }
        )
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        assert data.get("ok") is True
        payload = data.get("results")
        assert isinstance(payload, dict)
        assert payload["index_name"] == "test_index"
        assert payload["dim"] == 128
        assert payload["index_params"] == {"index_type": "FLAT"}


@pytest.mark.asyncio
async def test_call_tool_create_index_invalid_args_type_error(mcp_server):
    async with Client(mcp_server) as client:
        with pytest.raises(Exception):
            await client.call_tool(
                "create_index",
                {
                    "index_name": "test_index",
                    "dim": 128,
                    "index_params": "invalid_params"  # Should be a dict
                }
            )


# ----------- Get Index List Tool Tests ----------- #
@pytest.mark.asyncio
async def test_tools_list_contains_get_index_list(mcp_server):
    async with Client(mcp_server) as client:
        tools = await client.list_tools()
        names = [t.name for t in tools]
        assert "get_index_list" in names


@pytest.mark.asyncio
async def test_call_tool_get_index_list_happy_path(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool("get_index_list", {})
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        assert data.get("ok") is True
        payload = data.get("results")
        assert payload == ["index_a", "index_b"]


# ----------- Get Index Info Tool Tests ----------- #
@pytest.mark.asyncio
async def test_tools_list_contains_get_index_info(mcp_server):
    async with Client(mcp_server) as client:
        tools = await client.list_tools()
        names = [t.name for t in tools]
        assert "get_index_info" in names


@pytest.mark.asyncio
async def test_call_tool_get_index_info_happy_path(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool(
            "get_index_info",
            {"index_name": "index_a"}
        )
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        assert data.get("ok") is True
        payload = data.get("results")
        assert payload["index_name"] == "index_a"
        assert payload["dim"] == 128
        assert payload["row_count"] == 42


@pytest.mark.asyncio
async def test_call_tool_get_index_info_missing_index(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool(
            "get_index_info",
            {"index_name": "unknown_index"}
        )
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        assert data.get("ok") is False
        assert "unknown_index" in data.get("error", "")


# ----------- Insert Tool Tests ----------- #
# Test cases for the 'insert' tool in the MCP server
@pytest.mark.asyncio
async def test_tools_list_contains_insert(mcp_server):
    # In-memory client: connects directly to the server instance without network/process
    async with Client(mcp_server) as client:
        tools = await client.list_tools()
        names = [t.name for t in tools]
        assert "insert" in names  # Only 'insert' tool is defined for now

# Happy Path Test
@pytest.mark.asyncio
async def test_call_tool_insert_happy_path(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool(
            "insert",
            {
                "index_name": "test_index",
                "vectors": [[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]],
                "metadata": [{"field1": "value1"}, {"field2": "value2"}]
            }
        )
        # FastMCP returns results as 'structured data + traditional content'.
        # Depending on implementation/version, accessors may differ, so we check both cases permissively
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        assert data.get("ok") is True
        payload = data.get("results")
        assert isinstance(payload, dict)
        assert payload["index_name"] == "test_index"
        assert payload["vectors"] == [[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]]

# Invalid Argument Type Test
@pytest.mark.asyncio
async def test_call_tool_insert_invalid_args_type_error(mcp_server):
    async with Client(mcp_server) as client:
        # Invalid parameter value for vectors
        with pytest.raises(Exception):
            await client.call_tool(
                "insert",
                {
                    "index_name": "test_index",
                    "vectors": "this_should_be_a_list_of_floats_lists_or_else",  # Invalid type
                    "metadata": [{"field1": "value1"}, {"field2": "value2"}]
                }
            )  # Expected to raise an exception due to invalid argument type

# ----------- Insert Tool Tests Finished ----------- #

# ----------- Search Tool Tests ----------- #
# Test cases for the 'search' tool in the MCP server
@pytest.mark.asyncio
async def test_tools_list_contains_search(mcp_server):
    # In-memory client: connects directly to the server instance without network/process
    async with Client(mcp_server) as client:
        tools = await client.list_tools()
        names = [t.name for t in tools]
        assert "search" in names  # Only 'search' tool is defined for now

# Happy Path Test
@pytest.mark.asyncio
async def test_call_tool_happy_path(mcp_server):
    async with Client(mcp_server) as client:
        result = await client.call_tool(
            "search",
            {
                "index_name": "test_index",
                "query": [0.1, 0.2, 0.3],
                "topk": 5
            }
        )
        # FastMCP returns results as 'structured data + traditional content'.
        # Depending on implementation/version, accessors may differ, so we check both cases permissively
        data = getattr(result, "data", None) or getattr(result, "structured", None) \
               or getattr(result, "structured_content", None)

        assert data is not None, "No data returned from tool call"
        # Check the expected structure of the returned data from FakeAdapter
        # (key names may vary based on the actual adapter implementation)
        # Expected format:
        # {
        #     "ok": bool,
        #     "results": Any,          # Present if ok is True
        #     "error": str            # Present if ok is False
        # }
        assert data.get("ok") is True
        assert data.get("results", [{}])[0].get("metadata", {}).get("fieldA") == "valueA"

# ----------- Search Tool Tests Finished ----------- #
