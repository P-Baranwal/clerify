import json
from http.server import BaseHTTPRequestHandler, HTTPServer

ANALYSIS = {
    "title": "Distributed consensus essay",
    "type": "Essay",
    "deadline": "2026-11-02",
    "difficulty": 7,
    "difficultyReasoning": "Multiple unfamiliar consensus protocols",
    "estimatedHoursReasoning": "4 subtasks x ~2hr each, user is comfortable with Go so -20%",
    "estimatedHours": 7.5,
    "priorityScore": 84,
    "priorityReasoning": "Due in 8 days and worth 40% of the grade",
    "checklist": ["Read Raft paper", "Outline three protocols", "Draft comparison", "Edit and cite"],
}

PLAN = [
    {"day": "Monday", "date": "2026-10-26", "blocks": [{"task": "Distributed consensus essay", "hours": 2, "slot": "Morning"}], "totalHours": 2, "note": ""},
    {"day": "Tuesday", "date": "2026-10-27", "blocks": [{"task": "Distributed consensus essay", "hours": 2, "slot": "Afternoon"}], "totalHours": 2, "note": "split the draft"},
]

WBS = [
    {"title": "Research existing API auth options", "estimatedHours": 3, "impactScore": 9},
    {"title": "Design token refresh flow", "estimatedHours": 2, "impactScore": 8},
    {"title": "Implement middleware", "estimatedHours": 5, "impactScore": 10},
]


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, payload):
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.endswith("/models"):
            self._send({"data": [{"id": "fake-model-1"}, {"id": "fake-model-2"}]})
        else:
            self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(length) or b"{}")
        if "system" in req:
            system = req["system"]
        else:
            system = next((m["content"] for m in req.get("messages", []) if m["role"] == "system"), "")

        if "health check" in system:
            content = "OK"
        elif "assignment metadata extractor" in system:
            content = "```json\n" + json.dumps(ANALYSIS) + "\n```"
        elif "work-planning" in system:
            content = json.dumps(PLAN)
        elif "project planner" in system:
            content = json.dumps(WBS)
        else:
            content = "{}"

        self._send({"choices": [{"message": {"content": content}}]})


if __name__ == "__main__":
    HTTPServer(("127.0.0.1", 8765), Handler).serve_forever()
