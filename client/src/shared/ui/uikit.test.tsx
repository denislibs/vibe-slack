// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { Button, IconButton, Input, Avatar, Modal } from "./index";

describe("ui-kit", () => {
  it("Button renders a variant and handles click", () => {
    const onClick = vi.fn();
    const { getByRole } = render(() => <Button variant="primary" onClick={onClick}>Go</Button>);
    const b = getByRole("button");
    b.click();
    expect(onClick).toHaveBeenCalled();
    expect(b.className).toBeTruthy();
  });
  it("Avatar shows initials derived from name", () => {
    const { getByText } = render(() => <Avatar name="alice" />);
    expect(getByText("A")).toBeTruthy();
  });
  it("Input forwards aria-label and value", () => {
    const { getByLabelText } = render(() => <Input aria-label="Email" value="x" onInput={() => {}} />);
    expect((getByLabelText("Email") as HTMLInputElement).value).toBe("x");
  });
  it("IconButton uses its label as aria-label and fires onClick", () => {
    const onClick = vi.fn();
    const { getByLabelText } = render(() => <IconButton label="Settings" onClick={onClick}>⚙</IconButton>);
    const b = getByLabelText("Settings");
    b.click();
    expect(onClick).toHaveBeenCalled();
  });
  it("Modal renders children when open and closes on backdrop click", () => {
    const onClose = vi.fn();
    const { getByText, getByTestId } = render(() => <Modal open onClose={onClose}><div>body</div></Modal>);
    expect(getByText("body")).toBeTruthy();
    getByTestId("modal-backdrop").click();
    expect(onClose).toHaveBeenCalled();
  });
  it("Modal renders nothing when closed", () => {
    const { queryByText } = render(() => <Modal open={false} onClose={() => {}}><div>hidden</div></Modal>);
    expect(queryByText("hidden")).toBeNull();
  });
});
