#!/bin/bash

go build -ldflags="-s -w"
scp linktree server:~
ssh server sudo mv linktree /usr/local/bin
