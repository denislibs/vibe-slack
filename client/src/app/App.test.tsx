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

  it("re-renders when the conversation store gains a message after mount", async () => {
    const conv = createConversationStore();
    const conn = createConnectionStore();
    conn.setStatus("online");
    const { queryByText, findByText } = render(() => (
      <App groupId="g1" conversation={conv} connection={conn} onSend={() => {}} />
    ));
    expect(queryByText("late message")).toBeNull();
    // Mutate the store AFTER mount — the DOM must update reactively.
    conv.addMessage("g1", { seq: 1, sender: "bob", text: "late message" });
    expect(await findByText("late message")).toBeTruthy();
  });

  it("re-renders when connection status changes after mount", async () => {
    const conv = createConversationStore();
    const conn = createConnectionStore();
    const { getByTestId } = render(() => (
      <App groupId="g1" conversation={conv} connection={conn} onSend={() => {}} />
    ));
    expect(getByTestId("status").textContent).toBe("offline");
    conn.setStatus("online");
    // findBy/await a microtask for Solid to flush.
    await Promise.resolve();
    expect(getByTestId("status").textContent).toBe("online");
  });
});
