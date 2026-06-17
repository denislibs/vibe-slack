// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { App } from "./App";
import { createConversationStore } from "../entities/conversation/store";
import { createConnectionStore } from "../entities/connection/store";

describe("App", () => {
  it("renders the chat page wired to injected stores + send handler", () => {
    const conv = createConversationStore();
    conv.addMessage("g1", { seq: 1, sender: "alice", text: "wired" });
    const conn = createConnectionStore();
    conn.setStatus("online");
    const onSend = vi.fn();

    const { getByText, getByTestId } = render(() => (
      <App groupId="g1" conversation={conv} connection={conn} onSend={onSend} />
    ));
    expect(getByText("wired")).toBeTruthy();
    expect(getByTestId("status").textContent).toBe("online");
  });
});
