1. Implement signalling, start with a simplified entry-gated signal system. (currently signals only mark direction for path selection, no aspects, gate nothing)
~~2. Implement SwitchBlocks for managing the switch state.~~
~~3. Refactor Interlocking code out of Dispatcher.~~
4. Refactor Simulation into discrete parts, TrainController, Logging and Debug Dumping (TrainController part done, check Logging/Debug Dumping)
~~5. Implement movement authority and MOVEMENT_AUTHORITY_END, SCHEDULE_END, etc. (ma/ScheduleEnd exist, but MovementAuthorized/MovementAuthorityEnded still empty stubs)~~
6. Rework reservation requests with priority requests & train priority. (waitingProceedRequests never drained at all right now, trains get stuck for good)
7. Update physics, currently extremely simple constant speed calculation. t = d/s
8. Temporary Speed Restrictions and Permanent Speed Restrictions
9. Level crossings & caution orders
~~10. Implement RequestPathToStation, currently just panics. Fall back to last signal if platform reservation fails, only if another exit exists, else keep queued.~~
~~11. Handle MovementAuthorityEnded (empty case rn), check if at platform, else pathfind to one.~~
~~12. Handle RouteGranted from Home Signal Approach too, not just normal grants.~~
13. Move Interlocking out of Dispatcher into World.
~~14. Deprecate TryReservePathToTrack once RequestPathToStation (10) works.~~
15. Cap movement authority at last available signal instead of granting whole path upfront, root cause of the coarse whole-leg locking thing.
16. Save the blocking edge on reservation failure so retry queue (6) can wake up the right train instead of retrying blind.
17. Implement station exit verifier for (10)
