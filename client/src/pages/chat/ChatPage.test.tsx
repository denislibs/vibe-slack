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

  it("renders messages and sends composed text via the onSend prop", () => {
    const props = baseProps();
    const { getByText, getByPlaceholderText, getByRole } = render(() => <ChatPage {...props} />);
    expect(getByText("hello")).toBeTruthy();

    const input = getByPlaceholderText("Message") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "hi back" } });
    fireEvent.click(getByRole("button", { name: "Send" }));
    expect(props.onSend).toHaveBeenCalledWith("hi back");
  });
});
