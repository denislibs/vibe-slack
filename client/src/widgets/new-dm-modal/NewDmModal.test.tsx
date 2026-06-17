// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { NewDmModal } from "./NewDmModal";

const results = [{ user_id: "u2", username: "bob", email: "bob@c" }];

describe("NewDmModal", () => {
  it("typing calls onQuery with the value", () => {
    const onQuery = vi.fn();
    const { getByLabelText } = render(() => (
      <NewDmModal open onClose={() => {}} results={[]} onQuery={onQuery} onPick={() => {}} />
    ));
    fireEvent.input(getByLabelText("Search people"), { target: { value: "bo" } });
    expect(onQuery).toHaveBeenCalledWith("bo");
  });

  it("clicking a result calls onPick with the username", () => {
    const onPick = vi.fn();
    const { getByText } = render(() => (
      <NewDmModal open onClose={() => {}} results={results} onQuery={() => {}} onPick={onPick} />
    ));
    fireEvent.click(getByText("bob"));
    expect(onPick).toHaveBeenCalledWith("bob");
  });
});
