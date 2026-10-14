1. Implement signalling, start with a simplified entry-gated signal system. (currently signals only mark direction for path selection, no aspects, gate nothing)
~~2. Implement SwitchBlocks for managing the switch state.~~
~~3. Refactor Interlocking code out of Dispatcher.~~
4. Refactor Simulation into discrete parts, TrainController, Logging and Debug Dumping (TrainController and Dispatcher done; logging/dumping still live in sim.go)
~~5. Implement movement authority and MOVEMENT_AUTHORITY_END, SCHEDULE_END, etc. (ma/ScheduleEnd exist, but MovementAuthorized/MovementAuthorityEnded still empty stubs)~~
6. Rework reservation requests with priority requests & train priority. (requests now wait on the resources that block them and are woken FIFO per resource; no train priorities yet)
7. Update physics, currently extremely simple constant speed calculation. t = d/s
8. Temporary Speed Restrictions and Permanent Speed Restrictions
9. Level crossings & caution orders
~~10. Implement RequestPathToStation, currently just panics. Fall back to last signal if platform reservation fails, only if another exit exists, else keep queued.~~
~~11. Handle MovementAuthorityEnded (empty case rn), check if at platform, else pathfind to one.~~
~~12. Handle RouteGranted from Home Signal Approach too, not just normal grants.~~
~~13. Move Interlocking out of Dispatcher into World.~~ (done differently: Sim owns the Interlocking and hands it to Dispatcher and TrainController; World stays static)
~~14. Deprecate TryReservePathToTrack once RequestPathToStation (10) works.~~
~~15. Cap movement authority at last available signal instead of granting whole path upfront, root cause of the coarse whole-leg locking thing.~~ (authority and reservations are one signal section at a time; platform ends count as signals)
~~16. Save the blocking edge on reservation failure so retry queue (6) can wake up the right train instead of retrying blind.~~ (des.WaitList, woken by ConflictError.Blockers)
~~17. Implement station exit verifier for (10)~~ (generalised into the safety check in sim/safety.go: no grant may leave a state where some train cannot finish its schedule)
18. Speed up the safety check for large worlds (cache static reachability per position/station, or check incrementally).
19. Model reversals so trains can change direction at termini and junctions.
