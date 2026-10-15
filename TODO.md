~~1. Implement signalling, start with a simplified entry-gated signal system.~~ (signals and platform ends bound the sections a movement authority covers, see 15; aspects left for 22)
~~2. Implement SwitchBlocks for managing the switch state.~~
~~3. Refactor Interlocking code out of Dispatcher.~~
4. ~~Refactor Simulation into discrete parts, TrainController, Logging and Debug Dumping (TrainController and Dispatcher done; logging/dumping still live in sim.go)~~
~~5. Implement movement authority and MOVEMENT_AUTHORITY_END, SCHEDULE_END, etc. (ma/ScheduleEnd exist, but MovementAuthorized/MovementAuthorityEnded still empty stubs)~~
~~6. Rework reservation requests with priority requests & train priority.~~ (waiting requests are woken highest priority first, then FIFO; priority = class priority from a JSON config + delay bands, changeable with Sim.SetPriorityAt; see sim/priority.go)
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
20. Precedence: hold a lower-priority train (e.g. in a loop) so an approaching higher-priority one can overtake or cross. Priority (6) only orders trains already waiting on the same track. Must not fight the safety check.
21. Train length: free a track only once the rear has cleared it (also stops switches unlocking under the tail). Needs several routes per train and a platform length check. Pairs with 7.
22. Signal aspects and requesting the next section on approach, once trains brake (7).
23. Overlap: hold track and switches beyond a stop signal. Needs a length-based carve-out of the next segment; only matters with 21 or 24.
24. SPADs and other failures: not modelled on purpose, they make a run probabilistic (see README). Would need a seeded random source and braking (7).
25. Automatic signals on plain line could skip the safety check (see 18).
26. Add the real line's skipped stations to the TPJ-KKDI world as non-stopping signalled points, for realistic headway. Low priority.
~~27. Each event hop costs des.MinDeltaTime (0.01 min), so a signal handoff takes 0.05 min. Let same-instant hops share a timestamp?~~
