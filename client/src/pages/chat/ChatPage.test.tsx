// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { ChatPage } from "./ChatPage";

function baseProps() {
  return {
    messages: [{ seq: 1, sender: "alice", text: "hello" }],
    status: "online",
    onSend: vi.fn(),
    workspaceName: "Acme Inc",
    userEmail: "alice@acme.test",
    theme: "dark" as const,
    onToggleTheme: vi.fn(),
    channels: [] as { id: string; name: string; visibility: string }[],
    dms: [] as { id: string; name: string }[],
    activeId: "",
    onSelect: vi.fn(),
  };
}

describe("ChatPage", () => {
  it("renders the workspace name in the shell", () => {
    const { getAllByText } = render(() => <ChatPage {...baseProps()} />);
    expect(getAllByText("Acme Inc").length).toBeGreaterThan(0);
  });

  it("toggles theme when the toggle button is clicked", () => {
    const props = baseProps();
    const { getByLabelText } = render(() => <ChatPage {...props} />);
    fireEvent.click(getByLabelText("Toggle theme"));
    expect(props.onToggleTheme).toHaveBeenCalled();
  });

  it("renders the connection status", () => {
    const { getByTestId } = render(() => <ChatPage {...baseProps()} />);
    expect(getByTestId("status").textContent).toContain("online");
  });

  it("renders messages and a wired-up composer", () => {
    const props = baseProps();
    const { getByText, getByRole } = render(() => <ChatPage {...props} />);
    expect(getByText("hello")).toBeTruthy();

    // The composer is a TipTap (ProseMirror) contenteditable, exposed as a
    // textbox named "Message". Sending is gated until there's content, so Send
    // starts disabled. (The typed-text → onSend path runs through ProseMirror,
    // which can't be driven reliably under jsdom — it's covered by an in-browser
    // check instead.)
    expect(getByRole("textbox", { name: "Message" })).toBeTruthy();
    expect(getByRole("button", { name: "Send" })).toHaveProperty("disabled", true);
  });
});
