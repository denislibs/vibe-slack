// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { WorkspacePage } from "./WorkspacePage";

describe("WorkspacePage", () => {
  const workspaces = [{ id: "w1", name: "Acme", slug: "acme", role: "owner" }];

  it("lists workspaces and selects one on click", () => {
    const onSelect = vi.fn();
    const { getByText } = render(() => (
      <WorkspacePage workspaces={workspaces} onSelect={onSelect} onCreate={vi.fn()} busy={false} />
    ));
    fireEvent.click(getByText("Acme"));
    expect(onSelect).toHaveBeenCalledWith("w1");
  });

  it("creates a workspace from the name input", () => {
    const onCreate = vi.fn();
    const { getByLabelText, getByText } = render(() => (
      <WorkspacePage workspaces={workspaces} onSelect={vi.fn()} onCreate={onCreate} busy={false} />
    ));
    fireEvent.input(getByLabelText("Workspace name"), { target: { value: "Globex" } });
    fireEvent.click(getByText("Create"));
    expect(onCreate).toHaveBeenCalledWith("Globex");
  });
});
