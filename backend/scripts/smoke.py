#!/usr/bin/env python3
"""Exercise a running workflowd instance using Python's standard library."""

import json
import sys
import time
import urllib.error
import urllib.request


BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080").rstrip("/")


def call(path, body=None, status=200):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(BASE + path, data=data, headers={"Content-Type": "application/json"})
    try:
        response = urllib.request.urlopen(request, timeout=5)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        result = json.load(response)
        if response.status != status:
            raise AssertionError(f"{path}: HTTP {response.status}, expected {status}: {result}")
        return result.get("data", result.get("error"))


def execute(workflow_id, value, expected):
    started = call("/api/v1/runs", {"workflowId": workflow_id, "definitionVersion": "1", "input": value}, 202)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        result = call("/api/v1/runs/" + started["runId"])
        if result["status"] == "SUCCEEDED":
            assert result["output"] == expected, result
            assert result["ephemeral"] and result["instanceId"] == started["instanceId"]
            assert all(node["status"] == "SUCCEEDED" for node in result["nodes"]), result
            print(f"PASS {workflow_id}: {json.dumps(result['output'], ensure_ascii=False)}")
            return
        assert result["status"] in ("PENDING", "RUNNING"), result
        time.sleep(0.02)
    raise AssertionError(f"{workflow_id}: run did not finish")


def main():
    assert call("/readyz")["status"] == "ready"
    workflows = call("/api/v1/workflows")["items"]
    assert {item["workflowId"] for item in workflows} >= {"text-demo", "order-investigation"}
    assert len(call("/api/v1/node-types")["items"]) >= 8
    for workflow_id in ("text-demo", "order-investigation"):
        view = call(f"/api/v1/workflows/{workflow_id}/versions/1")
        assert len(view["nodes"]) == len(view["edges"]) == 4
        assert len(view["nodeTypes"]) == 4
        assert all(node["timeoutMs"] == 30000 for node in view["nodes"])
    execute("text-demo", {"text": " Hello "}, {"upper": "HELLO", "lower": "hello"})
    execute("order-investigation", {"orderId": "demo-1001"}, {"consistent": False, "issues": ["订单已发货，但配送尚未进入运输状态"]})
    execute("order-investigation", {"orderId": "demo-1002"}, {"consistent": True, "issues": []})
    error = call("/api/v1/runs", {"workflowId": "text-demo", "definitionVersion": "1", "input": {"text": 1}}, 422)
    assert error["code"] == "INPUT_INVALID"
    assert call("/api/v1/runs/missing", status=404)["code"] == "RUN_NOT_FOUND"
    print("PASS discovery, compiled graphs, node catalog and HTTP error contracts")


if __name__ == "__main__":
    main()
