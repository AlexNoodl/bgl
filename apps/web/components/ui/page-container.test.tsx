import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { PageContainer } from "./page-container";

describe("PageContainer", () => {
  it("renders its children", () => {
    render(
      <PageContainer>
        <p>content</p>
      </PageContainer>,
    );
    expect(screen.getByText("content")).toBeInTheDocument();
  });

  it("merges a caller className with its own instead of dropping either", () => {
    render(<PageContainer data-testid="container" className="py-0" />);
    const el = screen.getByTestId("container");
    expect(el.className).toContain("mx-auto");
    expect(el.className).toContain("py-0");
    expect(el.className).not.toContain("py-8");
  });
});
