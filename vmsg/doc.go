// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file was auto-generated via go generate.
// DO NOT UPDATE MANUALLY

/*
Runs the vanadium messenger service.

Usage:
   vmsg [flags] <command>

The vmsg commands are:
   node        Run the standalone node
   chat        Run chat demo application
   robot       Run a robot node that sends random messages every few seconds
   help        Display help for commands or topics

The vmsg flags are:
 -advertisement-id=
   The advertisement ID to use. If left empty, a random one is generated.
 -enable-local-discovery=true
   Whether local discovery, i.e. using mDNS and/or BLE, should be enabled.
 -encryption-key=This is not secure!!!
   Messages are encrypted with AES256 using this key
 -global-discovery-paths=
   A comma-separated list of namespace paths to use for global discovery.
 -max-active-peers=3
   The maximum number of peers to send updates to concurrently.
 -max-hops=10
   The maximum number of hops that a message can go through.
 -rate-acl-in=[{"acl":{"In":["..."]},"limit":10}]
   The RateAcl to authorize incoming RPCs, in JSON format
 -rate-acl-out=[{"acl":{"In":["..."]},"limit":10}]
   The RateAcl to authorize outgoing RPCs, in JSON format
 -rate-acl-sender=[{"acl":{"In":["..."]},"limit":10}]
   The RateAcl to authorize the sender of incoming messages, in JSON format
 -store-dir=
   The name of the local directory where to store the messages.

The global flags are:
 -alsologtostderr=true
   log to standard error as well as files
 -log_backtrace_at=:0
   when logging hits line file:N, emit a stack trace
 -log_dir=
   if non-empty, write log files to this directory
 -logtostderr=false
   log to standard error instead of files
 -max_stack_buf_size=4292608
   max size in bytes of the buffer to use for logging stack traces
 -metadata=<just specify -metadata to activate>
   Displays metadata for the program and exits.
 -stderrthreshold=2
   logs at or above this threshold go to stderr
 -time=false
   Dump timing information to stderr before exiting the program.
 -v=0
   log level for V logs
 -v23.credentials=
   directory to use for storing security credentials
 -v23.i18n-catalogue=
   18n catalogue files to load, comma separated
 -v23.namespace.root=[/(dev.v.io:r:vprod:service:mounttabled)@ns.dev.v.io:8101]
   local namespace root; can be repeated to provided multiple roots
 -v23.permissions.file=map[]
   specify a perms file as <name>:<permsfile>
 -v23.permissions.literal=
   explicitly specify the runtime perms as a JSON-encoded access.Permissions.
   Overrides all --v23.permissions.file flags.
 -v23.proxy=
   object name of proxy service to use to export services across network
   boundaries
 -v23.tcp.address=
   address to listen on
 -v23.tcp.protocol=wsh
   protocol to listen with
 -v23.vtrace.cache-size=1024
   The number of vtrace traces to store in memory.
 -v23.vtrace.collect-regexp=
   Spans and annotations that match this regular expression will trigger trace
   collection.
 -v23.vtrace.dump-on-shutdown=true
   If true, dump all stored traces on runtime shutdown.
 -v23.vtrace.sample-rate=0
   Rate (from 0.0 to 1.0) to sample vtrace traces.
 -v23.vtrace.v=0
   The verbosity level of the log messages to be captured in traces
 -vmodule=
   comma-separated list of globpattern=N settings for filename-filtered logging
   (without the .go suffix).  E.g. foo/bar/baz.go is matched by patterns baz or
   *az or b* but not by bar/baz or baz.go or az or b.*
 -vpath=
   comma-separated list of regexppattern=N settings for file pathname-filtered
   logging (without the .go suffix).  E.g. foo/bar/baz.go is matched by patterns
   foo/bar/baz or fo.*az or oo/ba or b.z but not by foo/bar/baz.go or fo*az
*/
package main
