// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { CreateChannelModal } from "./CreateChannelModal";

describe("CreateChannelModal", () => {
  it("collects name + private visibility and calls onCreate", () => {
    const onCreate = vi.fn();
    const { getByLabelText, getByText } = render(() => (
      <CreateChannelModal open onClose={() => {}} onCreate={onCreate} />
    ));

    const input = getByLabelText("Channel name") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "x" } });
    fireEvent.click(getByLabelText("Private"));
    fireEvent.click(getByText("Create"));

    expect(onCreate).toHaveBeenCalledWith("x", "private");
  });

  it("does not create when the name is empty", () => {
    const onCreate = vi.fn();
    const { getByText } = render(() => (
      <CreateChannelModal open onClose={() => {}} onCreate={onCreate} />
    ));
    fireEvent.click(getByText("Create"));
    expect(onCreate).not.toHaveBeenCalled();
  });
});
