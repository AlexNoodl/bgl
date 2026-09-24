import type { HTMLAttributes } from "react";

import { cn } from "@/lib/utils";

export function PageContainer({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "mx-auto w-full px-4 py-8 tablet:max-w-tablet tablet:px-8 desktop:max-w-desktop desktop:px-16",
        className,
      )}
      {...props}
    />
  );
}
