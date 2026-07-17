import { NavLink } from "react-router";

import Badge from "@/components/elements/Badge";
import { useUsersStatsQuery } from "@/hooks/queries/useUsersStatsQuery";
import { cn } from "@/utils/cn";
import { NAV_LIST, PATH_LIST } from "@/constants/commonConstants";

/**
 * MainNav renders the console navigation (SC-03 callouts 5-7) — a
 * vertical sidebar (the layout is fixed desktop-width; no responsive
 * horizontal variant).
 */
const MainNav = () => {
  const { data: stats } = useUsersStatsQuery();

  return (
    <nav className="flex w-36 shrink-0 flex-col gap-1">
      {NAV_LIST.map(({ title, url }) => (
        <NavLink
          key={url}
          to={url}
          className={({ isActive }) =>
            cn(
              "rounded-md px-3 py-2 text-sm whitespace-nowrap",
              isActive
                ? "bg-surface text-foreground font-medium"
                : "text-muted-foreground hover:text-foreground",
            )
          }
        >
          <span className="flex items-center gap-2">
            {title}
            {url === PATH_LIST.users && (
              <Badge value={stats?.invitePending ?? 0} className="ml-auto" />
            )}
          </span>
        </NavLink>
      ))}
    </nav>
  );
};

export default MainNav;
