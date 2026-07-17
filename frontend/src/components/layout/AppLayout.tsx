import { Outlet } from "react-router";

import MainNav from "@/components/navigation/MainNav";
import Navbar from "@/components/navigation/Navbar";

/**
 * AppLayout is the console app shell (SC-03): navbar, navigation, and routed
 * content. The body area is capped at the 1380px content width; beyond that
 * the page background fills the viewport.
 */
const AppLayout = () => {
  return (
    <div className="flex min-h-screen flex-col">
      <Navbar />
      <div className="max-w-content mx-auto flex w-full flex-1 flex-row gap-8 px-6 py-6">
        <MainNav />
        <main className="min-w-0 flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
};

export default AppLayout;
