"""Thin helpers over the VectorCraft MCP server (see mcp.py); the server starts on first use."""
import json
_m = None


def _mcp():
    global _m
    if _m is None:
        from mcp import MCP
        _m = MCP()
    return _m


def tool(_tool, **k):
    r = _mcp().call(_tool, k)
    if 'error' in r or r.get('isError'):
        raise SystemExit(f"{_tool}: {r}")
    t = r['content'][0].get('text', '')
    try:
        return json.loads(t)
    except Exception:
        return t


def run(c, p=None):
    return tool('run_command', command=c, params=p or {})
