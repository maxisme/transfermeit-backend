#!/bin/bash
git pull
export $(grep -v '^#' .dev.env | xargs)
docker stack deploy -c stack.yml dev-transfmeit-backend