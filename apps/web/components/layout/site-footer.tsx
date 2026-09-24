import { PageContainer } from "@/components/ui/page-container";

export function SiteFooter() {
  return (
    <footer className="border-t border-border bg-background">
      <PageContainer className="flex h-14 items-center justify-between py-0 text-14 text-muted-foreground">
        <p>&copy; {new Date().getFullYear()} BGL</p>
        <p>Your video games library</p>
      </PageContainer>
    </footer>
  );
}
