FROM       grafana/bigtable-backup:master-18e7589@sha256:7ad47b8cfcf48bb9a56903745d56fa22c2ee0a0f8005b1a6d50fe4da036dda5d
RUN        apk add --update --no-cache python3 python3-dev git \
            && pip3 install --no-cache-dir --upgrade pip
COPY       bigtable-backup.py bigtable-backup.py
COPY       requirements.txt requirements.txt
RUN        pip3 install -r requirements.txt
ENTRYPOINT ["usr/bin/python3", "bigtable-backup.py"]
