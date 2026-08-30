# StablePay Agentic Eval Workspace

This directory holds the implementation scaffolding for StablePay agentic value tests.

Current focus:

- `TASK-01` failure taxonomy contract
- `TASK-02` latency event schema
- `TASK-03` task JSON v2 schema
- `TASK-04` normalized plugin/backend fixture formats
- `TASK-05` final-state assertion adapter contract

Directory guide:

- `docs/`: PRD, TRD, and implementation notes
- `schemas/`: JSON schema contracts for tasks, failures, latency, and assertions
- `fixtures/`: normalized sample fixtures for plugin and backend state
- `tasks/`: sample tasks that validate the task schema contract

Execution note:

- authoring can happen from Windows or WSL through `/mnt/d/MyLab/StablePay`
- live eval runs should still be labeled separately as `live_wsl`
