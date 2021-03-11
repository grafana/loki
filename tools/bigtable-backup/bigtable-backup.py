import argparse
import subprocess
import time
import json

from datetime import datetime, timedelta
import pytz
from prometheus_client import CollectorRegistry, Gauge, push_to_gateway

registry = CollectorRegistry()
bigtable_backup_job_last_run_seconds = Gauge('bigtable_backup_job_last_run_seconds', 'Last time a bigtable backup job ran at.', registry=registry)
bigtable_backup_job_last_success_seconds = Gauge('bigtable_backup_job_last_success_seconds', 'Last time a bigtable backup job successfully finished.', registry=registry)
bigtable_backup_job_runtime_seconds =  Gauge('bigtable_backup_job_runtime_seconds', 'Runtime of last successfully finished bigtable backup job.', registry=registry)
bigtable_backup_job_backups_created = Gauge('bigtable_backup_job_backups_created', 'Number of backups created during last run.', registry=registry)
bigtable_backup_job_last_active_table_backup_time_seconds = Gauge('bigtable_backup_job_last_active_table_backup_time_seconds', 'Last time an active table was backed up at.', registry=registry)

job_backup_active_periodic_table = "backup-active-periodic-table"
job_ensure_backups = "ensure-backups"

def secs_to_periodic_table_number(periodic_secs):
    return time.time() / periodic_secs


def backup_active_periodic_table(args):
    push_job_started_metric(args.prom_push_gateway_endpoint, args.namespace, job_backup_active_periodic_table)
    start_time = time.time()

    table_id = args.bigtable_table_id_prefix + str(int(time.time() / args.periodic_table_duration))
    create_backup(table_id, args)

    bigtable_backup_job_last_active_table_backup_time_seconds.set_to_current_time()
    push_job_finished_metric(args.prom_push_gateway_endpoint, args.namespace, job_backup_active_periodic_table, int(time.time() - start_time))


def ensure_backups(args):
    push_job_started_metric(args.prom_push_gateway_endpoint, args.namespace, job_ensure_backups)
    start_time = time.time()

    if (args.duration == None and args.period_from == None) or (args.duration != None and args.period_from != None):
        raise ValueError("Either of --duration or --periodic-table-duration must be set")

    backups = list_backups(args.destination_path)

    if args.period_from == None:
        period_from = datetime.utcnow() - timedelta(seconds=args.duration)
        args.period_from = valid_date(period_from.strftime("%Y-%m-%d"))
        args.period_to = valid_date(datetime.utcnow().strftime("%Y-%m-%d"))

    oldest_table_number = int(args.period_from.timestamp() / args.periodic_table_duration)
    newest_table_number = int(args.period_to.timestamp() / args.periodic_table_duration)
    active_table_number = int(time.time() / args.periodic_table_duration)

    print("Checking right backups exist")
    table_number_to_check = oldest_table_number
    while table_number_to_check <= newest_table_number:
        table_id = args.bigtable_table_id_prefix + str(table_number_to_check)
        table_number_to_check += 1
        if table_id not in backups:
            print("backup for {} not found".format(table_id))
            create_backup(table_id, args)
        if table_id == active_table_number:
            bigtable_backup_job_last_active_table_backup_time_seconds.set_to_current_time()

    num_backups_deleted = 0

    print("Checking whether all the backups are created after their period is over")
    for table_id, timestamps in backups.items():
        table_number = int(table_id.rsplit("_", 1)[-1])
        last_timestamp_from_table_number = find_last_timestamp_from_table_number(table_number,
                                                                                 args.periodic_table_duration)

        # Checking whether backup is created after last timestamp of tables period.
        if last_timestamp_from_table_number > timestamps[-1]:
            create_backup(table_id, args)

    # list backups again to consider for deletion of unwanted backups since new backups might have been created above
    backups = list_backups(args.destination_path)

    print("Deleting old unwanted backups")
    for table_id, timestamps in backups.items():
        table_number = int(table_id.rsplit("_", 1)[-1])

        # Retain only most recent backup for non active table
        if table_number != active_table_number and len(timestamps) > 1:
            for timestamp in timestamps[:-1]:
                delete_backup(table_id, str(timestamp), args)
                num_backups_deleted += 1

    if args.delete_out_of_range_backups:
        num_backups_deleted += delete_out_of_range_backups(oldest_table_number, newest_table_number, backups, args)

    set_ensure_backups_specific_metrics(args, num_backups_deleted, active_table_number)
    push_job_finished_metric(args.prom_push_gateway_endpoint, args.namespace, job_ensure_backups, int(time.time() - start_time))

def find_last_timestamp_from_table_number(table_number, periodic_secs):
    return ((table_number + 1) * periodic_secs) - 1

def list_backups(backup_path):
    popen = subprocess.Popen(['bigtable-backup', 'list-backups', '-ojson', '--backup-path', backup_path],
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    popen.wait()

    return json.loads(popen.stdout.readline())

def set_ensure_backups_specific_metrics(args, num_backups_deleted, active_table_number):
    # ensure-backups job specific metrics
    bigtable_backup_job_tables_backed_up = Gauge('bigtable_backup_job_tables_backed_up', 'Number of active and inactive tables backed up.', ['kind'], registry=registry)
    bigtable_backup_job_backups = Gauge('bigtable_backup_job_backups', 'Number of backups for all active and inactive tables.', ['kind'], registry=registry)
    bigtable_backup_job_backups_deleted = Gauge('bigtable_backup_job_backups_deleted', 'Number of backups deleted during last run.', registry=registry)
    bigtable_backup_job_expected_inactive_table_backups = Gauge('bigtable_backup_job_expected_inactive_table_backups', 'Expected number of backups for inactive tables.', registry=registry)

    duration = args.duration
    if args.duration == None:
        duration = (args.period_to - args.period_from).seconds

    # there should be 1 backup per inactive table
    bigtable_backup_job_expected_inactive_table_backups.set(int(duration/args.periodic_table_duration))

    bigtable_backup_job_backups_deleted.set(num_backups_deleted)

    backups = list_backups(args.destination_path)
    inactive_table_backups_count = 0

    # setting sum of number of backups per table
    for table_id, timestamps in backups.items():
        table_number = int(table_id.rsplit("_", 1)[-1])

        label = 'active'
        if active_table_number != table_number:
            label = 'inactive'
            inactive_table_backups_count += 1

        bigtable_backup_job_backups.labels(label).inc(len(timestamps))

    bigtable_backup_job_tables_backed_up.labels('inactive').set(inactive_table_backups_count)
    if len(backups) != inactive_table_backups_count:
        bigtable_backup_job_tables_backed_up.labels('active').set(1)

def valid_date(s):
    try:
        dt = datetime.utcnow().strptime(s, "%Y-%m-%d")
        utc = pytz.timezone('UTC')
        return utc.localize(dt)
    except ValueError:
        msg = "Not a valid date: '{0}'.".format(s)
        raise argparse.ArgumentTypeError(msg)


def valid_duration(s):
    try:
        return int(s) * 3600
    except ValueError:
        msg = "Not a valid duration: '{0}'.".format(s)
        raise argparse.ArgumentTypeError(msg)


def valid_table_id_prefix(s):
    if not str(s).endswith("_"):
        return str(s) + "_"


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
        bigtable_backup_job_backups_created.inc(1)

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

def delete_out_of_range_backups(oldest_table_number, newest_table_number, backups, args):
    num_backups_deleted = 0
    for table_id, timestamps in backups.items():
        table_number = int(table_id.rsplit("_", 1)[-1])
        if table_number < oldest_table_number or table_number > newest_table_number:
            for timestamp in timestamps:
                delete_backup(table_id, str(timestamp), args)
                num_backups_deleted += 1

    return num_backups_deleted


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
    parser.add_argument("--periodic-table-duration", required=True, type=valid_duration,
                        help="Periodic config set for loki tables in hours")
    parser.add_argument("--prom-push-gateway-endpoint", default="localhost:9091", help="Endpoint where metrics are to be pushed")
    parser.add_argument("--namespace", default="default", help="namespace while reporting metrics")

    backup_active_periodic_table_parser = subparser.add_parser(job_backup_active_periodic_table,
                                                               help="Backup active periodic table")
    backup_active_periodic_table_parser.set_defaults(func=backup_active_periodic_table)

    ensure_backups_parser = subparser.add_parser(job_ensure_backups,
                                                               help="Ensure backups of right tables exist")
    ensure_backups_parser.add_argument('--duration', help="Duration in hours for which backups should exist. "
                                                          "Must not be set with --period-from and --period-to", type=valid_duration)
    ensure_backups_parser.add_argument('--period-from', type=valid_date, help="Backups should exist starting from the date. Must not be set with --duration")
    ensure_backups_parser.add_argument('--period-to', type=valid_date,
                                                     default=datetime.utcnow().strftime("%Y-%m-%d"))
    ensure_backups_parser.add_argument('--delete-out-of-range-backups', help="Delete backups which are out of range of duration for which backups are being ensured",
                                       default=False)
    ensure_backups_parser.set_defaults(func=ensure_backups)

    args = parser.parse_args()

    args.func(args)


if __name__ == "__main__":
    main()
