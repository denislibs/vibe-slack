// @vitest-environment jsdom
import { render } from "@solidjs/testing-library";
import { describe, it, expect } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the app shell", () => {
    const { getByText } = render(() => <App />);
    expect(getByText("Messenger")).toBeTruthy();
  });
});
