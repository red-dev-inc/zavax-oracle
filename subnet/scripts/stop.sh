#!/bin/bash
ps aux | grep network-runner | grep -v grep | awk '{print $2}' | xargs kill && pkill -9 -f vu3xjfNfwJcNq1c4yFzvjF2hz6t2HZ4uHaWWQJvo27oyF6czX
