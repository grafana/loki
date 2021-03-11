FROM       grafana/bigtable-backup:master-18e7589
RUN        apk add --update --no-cache python3 python3-dev git \
            && pip3 install --no-cache-dir --upgrade pip
COPY       bigtable-backup.py bigtable-backup.py
COPY       requirements.txt requirements.txt
RUN        pip3 install -r requirements.txt
ENTRYPOINT ["usr/bin/python3", "bigtable-backup.py"]
