Create dispatcher -> move all controllers to dispatcher
Create train controller

train controller > sim
dispatcher > sim

1. Implement proper signalling, start with a simplified block section.
2. Implement SwitchBlocks for managing the switch state.
3. Refactor dispatcher into InterlockingManager & ReservationManager (I'm wary of names, feel free to suggest names)
4. Refactor Simulation into discrete parts, TrainController, Logging and Debug Dumping
5. Implement movement authority and MOVEMENT_AUTHORITY_END, SCHEDULE_END, etc.
6. Rework reservation requests with priority requests & train priority.
7. Update physics, currently extremely simple constant speed calculation. t = d/s
8. Temporary Speed Restrictions and Permanent Speed Restrictions
9. Level crossings & caution orders