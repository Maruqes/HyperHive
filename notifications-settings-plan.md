# Plan: Notification settings

**Generated**: 2026-10-28  
**Estimated complexity**: Low

## Overview
Move notification registration from Logs to Settings and add a confirmed action that deletes all stored notification history while retaining push subscriptions.

## Sprint 1: Backend
- Add an authenticated DELETE endpoint to the existing notification route, using the request context and returning an error if deletion fails.
- Test deletion, repeated deletion, preservation of subscriptions, and authorization using an isolated database.
- Validate with focused Go tests.

## Sprint 2: Settings
- Move the existing browser registration action into the sidebar Settings dialog.
- Add a clear action with an explicit confirmation, pending state, and success or error feedback.
- Remove the old Logs button and unused code.
- Validate TypeScript and the frontend build where available.

## Risks and rollback
- Clearing permanently removes all notification history, including rows outside the current date filter; confirmation makes the scope explicit. New notifications can still arrive afterward.
- Roll back the frontend and backend changes together. Deleted history can only be recovered from a database backup.
