import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { SiteShell } from "./site-shell";

describe("SiteShell", () => {
  it("renders the shared header and footer around the page content", () => {
    render(
      <SiteShell>
        <p>page content</p>
      </SiteShell>,
    );

    expect(screen.getByRole("link", { name: "BGL" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Home" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Войти" })).toHaveAttribute("href", "/login");
    expect(screen.getByText("page content")).toBeInTheDocument();
    // Для футера
    // expect(screen.getByText(`© ${new Date().getFullYear()} BGL`)).toBeInTheDocument();
  });
});
