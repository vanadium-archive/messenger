#!/bin/bash
#
# This script builds the vmsg binary for arm.

jiri profile-v23 install --target=arm-linux@ v23:base

GOARCH=arm jiri go install -a -ldflags '-extldflags "-lpthread -static"' messenger/vmsg
