# Knowledge Adaptation Skill

This skill teaches agents how to adapt explanations to a user's knowledge profile.

- At session start: call the MCP tool `knowledge_profile_get` to read the user's active level.
- Adjust explanation depth, vocabulary, and assumed prior knowledge based on `level` and `display_scale`.
- In `inferred` and `hybrid` modes: after each interaction, call `knowledge_profile_record_signal` to record observed signals.
- Offer calibration (quiz) when no profile exists.
- Respect `self-declared` mode: never auto-update the profile.
