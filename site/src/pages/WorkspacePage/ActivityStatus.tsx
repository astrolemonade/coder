import { type FC } from "react";
import dayjs from "dayjs";
import relativeTime from "dayjs/plugin/relativeTime";
import type { Workspace } from "api/typesGenerated";
import { useTime } from "hooks/useTime";
import { Pill } from "components/Pill/Pill";

dayjs.extend(relativeTime);

interface ActivityStatusProps {
  workspace: Workspace;
}

export const ActivityStatus: FC<ActivityStatusProps> = ({ workspace }) => {
  const builtAt = dayjs(workspace.latest_build.updated_at);
  const usedAt = dayjs(workspace.last_used_at);
  const now = dayjs();

  // This needs to compare to `usedAt` instead of `now`, because the "grace period" for
  // marking a workspace as "Connected" is a lot longer. If you compared `builtAt` to `now`,
  // you could end up switching from "Ready" to "Connected" without ever actually connecting.
  const isBuiltRecently = builtAt.isAfter(usedAt.subtract(1, "second"));
  const isUsedRecently = usedAt.isAfter(now.subtract(15, "minute"));

  useTime(isUsedRecently);

  switch (workspace.latest_build.status) {
    // If the build is still "fresh", it'll be a while before the `last_used_at` gets bumped in
    // a significant way by the agent, so just label it as ready instead of connected.
    // Wait until `last_used_at` is after the time that the build finished, _and_ still
    // make sure to check that it's recent, so that we don't show "Ready" indefinitely.
    case isBuiltRecently &&
      isUsedRecently &&
      workspace.health.healthy &&
      "running":
      return <Pill type="active">Ready</Pill>;
    // Since the agent reports on a 10m interval, we present any connection within that period
    // plus a little wiggle room as an active connection.
    case isUsedRecently && "running":
      return <Pill type="active">Connected</Pill>;
    case "running":
    case "stopping":
    case "stopped":
      return <Pill type="inactive">Not connected</Pill>;
  }

  return null;
};