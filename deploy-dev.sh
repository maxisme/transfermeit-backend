#!/bin/bash
git pull
export ENV_FILE=".dev.env"
export $(grep -v '^#' $ENV_FILE | xargs)
docker stack deploy -c stack.yml dev-transfmeit-backend