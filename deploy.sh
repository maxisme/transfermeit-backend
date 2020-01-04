#!/bin/bash
# cd /root/ && git clone https://github.com/maxisme/transfermeit-backend
# $ visudo
# jenk ALL = NOPASSWD: /bin/bash /root/transfermeit-backend/deploy.sh
# remember to add .env file

cd $(dirname "$0")

git fetch &> /dev/null
diffs=$(git diff master origin/master)

if [ ! -z "$diffs" ]
then
    echo "Pulling code from GitHub..."
    git checkout master
    git pull origin master

    # run db migrations
    docker-compose up flyway

    # update server
    docker-compose build app
    docker-compose up --no-deps -d app

    # kill all unused dockers
    docker system prune -f
else
    echo "Already up to date"
fi