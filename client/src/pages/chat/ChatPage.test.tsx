// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { ChatPage } from "./ChatPage";

describe("ChatPage", () => {
  it("renders messages and sends composed text via the onSend prop", async () => {
    const onSend = vi.fn();
    const messages = [{ seq: 1, sender: "alice", text: "hello" }];
    const { getByText, getByRole } = render(() => (
      <ChatPage messages={messages} status="online" onSend={onSend} />
    ));
    expect(getByText("hello")).toBeTruthy();

    const input = getByRole("textbox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "hi back" } });
    fireEvent.click(getByRole("button"));
    expect(onSend).toHaveBeenCalledWith("hi back");
  });
});
