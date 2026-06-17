import { render } from "solid-js/web";
import { App } from "./app/App";
import { bootstrap } from "./app/bootstrap";

const root = document.getElementById("root");
if (root) {
  const { orchestrator, conversation, connection } = bootstrap("device");
  // TODO: call orchestrator.connect(token) once a session token exists (OPAQUE-client plan).
  render(() => (
    <App
      groupId="g1"
      conversation={conversation}
      connection={connection}
      onSend={(text) => orchestrator.sendText("g1", text)}
    />
  ), root);
}
