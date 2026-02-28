#!/bin/sh
systemctl stop smarthomeentry-agent 2>/dev/null || true
systemctl disable smarthomeentry-agent 2>/dev/null || true
