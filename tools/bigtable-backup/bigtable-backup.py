import argparse
import subprocess
import time
import json

from datetime import datetime, timedelta
import pytz
from prometheus_client import CollectorRegistry, Gauge, push_to_gateway

registry = CollectorRegistry()
bigtable_backup_job_last_run_seconds = Gauge('bigtable_backup_job_last_run_seconds', 'Last time a bigtable backup job ran at', registry=registry)
bigtable_backup_job_last_success_seconds = Gauge('bigtable_backup_job_last_success_seconds', 'Last time a bigtable backup job successfully finished', registry=registry)
bigtable_backup_job_runtime_seconds =  Gauge('bigtable_backup_job_runtime_seconds', 'Runtime of last successfully finished bigtable backup job', registry=registry)
bigtable_backup_job_backups_created = Gauge('bigtable_backup_job_backups_created', 'Number of backups created during last run', registry=registry)

job_backup_active_periodic_table = "backup-active-periodic-table"
job_ensure_backups = "ensure-backups"

def secs_to_periodic_table_number(periodic_secs):
    return time.time() / periodic_secs


def backup_active_periodic_table(args):
    push_job_started_metric(args.prom_push_gateway_endpoint, args.namespace, job_backup_active_periodic_table)
    start_time = time.time()

    table_id = args.bigtable_table_id_prefix + str(int(time.time() / args.periodic_table_duration))
    create_backup(table_id, args)

    push_job_finished_metric(args.prom_push_gateway_endpoint, args.namespace, job_backup_active_periodic_table, int(time.time() - start_time))


def ensure_backups(args):
    push_job_started_metric(args.prom_push_gateway_endpoint, args.namespace, job_ensure_backups)
    start_time = time.time()

    # ensure-backups job specific metrics
    bigtable_backup_job_num_tables_backed_up = Gauge('bigtable_backup_job_num_tables_backed_up', 'Number of table backups found during last run', registry=registry)
    bigtable_backup_job_num_backup_ups = Gauge('bigtable_backup_job_num_backup_ups', 'Sum of number of backups per table found during last run', registry=registry)

    # Read all the existing backups
    popen = subprocess.Popen(['bigtable-backup', 'list-backups', '-ojson', '--backup-path', args.destination_path],
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    popen.wait()

    # build and push metrics related to existing backups
    backups = json.loads(popen.stdout.readline())
    if (args.duration == None and args.period_from == None) or (args.duration != None and args.period_from != None):
        raise ValueError("Either of --duration or --periodic-table-duration must be set")

    bigtable_backup_job_num_tables_backed_up.set(len(backups))
    for __, timestamps in backups.items():
        bigtable_backup_job_num_backup_ups.inc(len(timestamps))

    push_metrics(args.prom_push_gateway_endpoint, args.namespace, job_ensure_backups)

    if args.period_from == None:
        period_from = datetime.utcnow() - timedelta(days=args.duration)
        args.period_from = valid_date(period_from.strftime("%Y-%m-%d"))
        args.period_to = valid_date(datetime.utcnow().strftime("%Y-%m-%d"))

    oldest_table_number = int(args.period_from.timestamp() / args.periodic_table_duration)
    newest_table_number = int(args.period_to.timestamp() / args.periodic_table_duration)
    active_table_number = time.time() / args.periodic_table_duration

    print("Checking right backups exist")
    while oldest_table_number <= newest_table_number:
        table_id = args.bigtable_table_id_prefix + str(oldest_table_number)
        oldest_table_number += 1
        if table_id not in backups:
            print("backup for {} not found".format(table_id))
            create_backup(table_id, args)
        bigtable_backup_job_backups_created.inc(1)
            
    print("Checking whether all the backups are created after their period is over and deleting old unwanted backups")
    for table_id, timestamps in backups.items():
        table_number = int(table_id.rsplit("_", 1)[-1])
        last_timestamp_from_table_number = find_last_timestamp_from_table_number(table_number,
                                                                                 args.periodic_table_duration)

        # Checking whether backup is created after last timestamp of tables period.
        if last_timestamp_from_table_number > timestamps[-1]:
            create_backup(table_id, args)

        # Retain only most recent backup for non active table
        if table_number != active_table_number and len(timestamps) > 1:
            for timestamp in timestamps[:-1]:
                delete_backup(table_id, str(timestamp), args)

    push_job_finished_metric(args.prom_push_gateway_endpoint, args.namespace, job_ensure_backups, int(time.time() - start_time))

def find_last_timestamp_from_table_number(table_number, periodic_secs):
    return ((table_number + 1) * periodic_secs) - 1


def valid_date(s):
    try:
        dt = datetime.utcnow().strptime(s, "%Y-%m-%d")
        utc = pytz.timezone('UTC')
        return utc.localize(dt)
    except ValueError:
        msg = "Not a valid date: '{0}'.".format(s)
        raise argparse.ArgumentTypeError(msg)


def valid_periodic_duration(s):
    try:
        return int(s) * 86400
    except ValueError:
        msg = "Not a valid duration: '{0}'.".format(s)
        raise argparse.ArgumentTypeError(msg)


def valid_table_id_prefix(s):
    if not str(s).endswith("_"):
        return str(s) + "_"

def valid_int(s):
    return int(s)


def create_backup(table_id, args):
    popen = subprocess.Popen(['bigtable-backup', 'create', '--bigtable-table-id-prefix', table_id,
                              '--temp-prefix', args.temp_prefix, '--bigtable-project-id', args.bigtable_project_id,
                              '--bigtable-instance-id', args.bigtable_instance_id, '--destination-path',
                              args.destination_path],
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    popen.wait()
    if popen.returncode != 0:
        raise Exception("Failed to create backup with error {}".format(b"".join(popen.stdout.readlines()).decode()))
    else:
        print("Backup created for table {}".format(table_id))


def delete_backup(table_id, timestamp, args):
    popen = subprocess.Popen(['bigtable-backup', 'delete-backup', '--bigtable-table-id', table_id,
                              '--backup-path', args.destination_path, "--backup-timestamp", timestamp],
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    popen.wait()
    if popen.returncode != 0:
        raise Exception("Failed to delete backup with error {}".format(b"".join(popen.stdout.readlines()).decode()))
    else:
        print(popen.stdout.readlines())

def push_job_started_metric(endpoint, namespace, job):
    try:
        bigtable_backup_job_last_run_seconds.set_to_current_time()
        push_to_gateway(endpoint, job="{}/{}".format(namespace, job), registry=registry)
    except Exception as e:
        print("failed to push metrics with error {}".format(e))

def push_job_finished_metric(endpoint, namespace, job, runtime):
    try:
        bigtable_backup_job_last_success_seconds.set_to_current_time()
        bigtable_backup_job_runtime_seconds.set(runtime)
        push_to_gateway(endpoint, job="{}/{}".format(namespace, job), registry=registry)
    except Exception as e:
        print("failed to push metrics with error {}".format(e))

def push_metrics(endpoint, namespace, job):
    try:
        push_to_gateway(endpoint, job="{}/{}".format(namespace, job), registry=registry)
    except Exception as e:
        print("failed to push metrics with error {}".format(e))


def main():
    parser = argparse.ArgumentParser()
    subparser = parser.add_subparsers(help="commands")
    parser.add_argument("--bigtable-project-id", required=True,
                        help="The ID of the GCP project of the Cloud Bigtable instance")
    parser.add_argument("--bigtable-instance-id", required=True,
                        help="The ID of the Cloud Bigtable instance that contains the tables")
    parser.add_argument("--bigtable-table-id-prefix", required=True, type=valid_table_id_prefix,
                        help="Prefix to build IDs of the tables using periodic-table-duration")
    parser.add_argument("--destination-path", required=True,
                        help="GCS path where data should be written. For example, gs://mybucket/somefolder/")
    parser.add_argument("--temp-prefix", required=True,
                        help="Path and filename prefix for writing temporary files. ex: gs://MyBucket/tmp")
    parser.add_argument("--periodic-table-duration", required=True, type=valid_periodic_duration,
                        help="Periodic config set for loki tables in days")
    parser.add_argument("--prom-push-gateway-endpoint", default="localhost:9091", help="Endpoint where metrics are to be pushed")
    parser.add_argument("--namespace", default="default", help="namespace while reporting metrics")

    backup_active_periodic_table_parser = subparser.add_parser(job_backup_active_periodic_table,
                                                               help="Backup active periodic table")
    backup_active_periodic_table_parser.set_defaults(func=backup_active_periodic_table)

    ensure_backups_parser = subparser.add_parser(job_ensure_backups,
                                                               help="Ensure backups of right tables exist")
    ensure_backups_parser.add_argument('--duration', help="Duration in previous consecutive days for which backups should exist. "
                                                          "Must not be set with --period-from and --period-to", type=valid_int)
    ensure_backups_parser.add_argument('--period-from', type=valid_date, help="Backups should exist starting from the date. Must not be set with --duration")
    ensure_backups_parser.add_argument('--period-to', type=valid_date,
                                                     default=datetime.utcnow().strftime("%Y-%m-%d"))
    ensure_backups_parser.set_defaults(func=ensure_backups)

    args = parser.parse_args()

    args.func(args)


if __name__ == "__main__":
    main()
