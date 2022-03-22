/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to you under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package phoenix

import (
	"math"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/apache/calcite-avatica-go/v5/errors"
	"github.com/apache/calcite-avatica-go/v5/internal"
	"github.com/apache/calcite-avatica-go/v5/message"
)

type Adapter struct {
}

func (a Adapter) GetPingStatement() string {
	return "SELECT 1"
}

func (a Adapter) GetColumnTypeDefinition(col *message.ColumnMetaData) *internal.Column {

	column := &internal.Column{
		Name:     col.ColumnName,
		TypeName: col.Type.Name,
		Nullable: col.Nullable != 0,
	}

	// Handle precision and length
	switch col.Type.Name {
	case "DECIMAL":

		precision := int64(col.Precision)

		if precision == 0 {
			precision = math.MaxInt64
		}

		scale := int64(col.Scale)

		if scale == 0 {
			scale = math.MaxInt64
		}

		column.PrecisionScale = &internal.PrecisionScale{
			Precision: precision,
			Scale:     scale,
		}
	case "VARCHAR", "CHAR", "BINARY":
		column.Length = int64(col.Precision)
	case "VARBINARY":
		column.Length = math.MaxInt64
	}

	// Handle scan types
	switch col.Type.Name {
	case "INTEGER", "UNSIGNED_INT", "BIGINT", "UNSIGNED_LONG", "TINYINT", "UNSIGNED_TINYINT", "SMALLINT", "UNSIGNED_SMALLINT":
		column.ScanType = reflect.TypeOf(int64(0))

	case "FLOAT", "UNSIGNED_FLOAT", "DOUBLE", "UNSIGNED_DOUBLE":
		column.ScanType = reflect.TypeOf(float64(0))

	case "DECIMAL", "VARCHAR", "CHAR":
		column.ScanType = reflect.TypeOf("")

	case "BOOLEAN":
		column.ScanType = reflect.TypeOf(false)

	case "TIME", "DATE", "TIMESTAMP", "UNSIGNED_TIME", "UNSIGNED_DATE", "UNSIGNED_TIMESTAMP":
		column.ScanType = reflect.TypeOf(time.Time{})

	case "BINARY", "VARBINARY":
		column.ScanType = reflect.TypeOf([]byte{})

	default:
		column.ScanType = reflect.TypeOf(new(interface{})).Elem()
	}

	// Handle rep type special cases for decimals, floats, date, time and timestamp
	switch col.Type.Name {
	case "DECIMAL":
		column.Rep = message.Rep_BIG_DECIMAL
	case "FLOAT":
		column.Rep = message.Rep_FLOAT
	case "UNSIGNED_FLOAT":
		column.Rep = message.Rep_FLOAT
	case "TIME", "UNSIGNED_TIME":
		column.Rep = message.Rep_JAVA_SQL_TIME
	case "DATE", "UNSIGNED_DATE":
		column.Rep = message.Rep_JAVA_SQL_DATE
	case "TIMESTAMP", "UNSIGNED_TIMESTAMP":
		column.Rep = message.Rep_JAVA_SQL_TIMESTAMP
	default:
		column.Rep = col.Type.Rep
	}

	return column
}

func (a Adapter) ErrorResponseToResponseError(err *message.ErrorResponse) errors.ResponseError {
	var (
		errorCode int
		sqlState  string
	)

	re := regexp.MustCompile(`ERROR (\d+) \(([0-9a-zA-Z]+)\)`)
	codes := re.FindStringSubmatch(err.ErrorMessage)

	if len(codes) > 1 {
		errorCode, _ = strconv.Atoi(codes[1])
	}

	if len(codes) > 2 {
		sqlState = codes[2]
	}

	return errors.ResponseError{
		Exceptions:   err.Exceptions,
		ErrorMessage: err.ErrorMessage,
		Severity:     int8(err.Severity),
		ErrorCode:    errors.ErrorCode(errorCode),
		SqlState:     errors.SQLState(sqlState),
		Metadata: &errors.RPCMetadata{
			ServerAddress: message.ServerAddressFromMetadata(err),
		},
		Name: errorCodeNames[uint32(errorCode)],
	}

}

var errorCodeNames = map[uint32]string{
	// Connection exceptions (errorcode 01, sqlstate 08)
	101: "io_exception",
	102: "malformed_connection_url",
	103: "cannot_establish_connection",

	// Data exceptions (errorcode 02, sqlstate 22)
	201: "illegal_data",
	202: "divide_by_zero",
	203: "type_mismatch",
	204: "value_in_upsert_not_constant",
	205: "malformed_url",
	206: "data_exceeds_max_capacity",
	207: "missing_max_length",
	208: "nonpositive_max_length",
	209: "decimal_precision_out_of_range",
	212: "server_arithmetic_error",
	213: "value_outside_range",
	214: "value_in_list_not_constant",
	215: "single_row_subquery_returns_multiple_rows",
	216: "subquery_returns_different_number_of_fields",
	217: "ambiguous_join_condition",
	218: "constraint_violation",

	301: "concurrent_table_mutation",
	302: "cannot_index_column_on_type",

	// Invalid cursor state (errorcode 04, sqlstate 24)
	401: "cursor_before_first_row",
	402: "cursor_past_last_row",

	// Syntax error or access rule violation (errorcode 05, sqlstate 42)
	501: "ambiguous_table",
	502: "ambiguous_column",
	503: "index_missing_pk_columns",
	504: "column_not_found",
	505: "read_only_table",
	506: "cannot_drop_pk",
	509: "primary_key_missing",
	510: "primary_key_already_exists",
	511: "order_by_not_in_select_distinct",
	512: "invalid_primary_key_constraint",
	513: "array_not_allowed_in_primary_key",
	514: "column_exist_in_def",
	515: "order_by_array_not_supported",
	516: "non_equality_array_comparison",
	517: "invalid_not_null_constraint",

	// Invalid transaction state (errorcode 05, sqlstate 25)
	518: "read_only_connection",
	519: "varbinary_array_not_supported",

	// Expression index exceptions
	520: "aggregate_expression_not_allowed_in_index",
	521: "non_deterministic_expression_not_allowed_in_index",
	522: "stateless_expression_not_allowed_in_index",

	// Transaction exceptions
	523: "transaction_conflict_exception",
	524: "transaction_exception",

	// Union all related errors
	525: "select_column_num_in_unionall_diffs",
	526: "select_column_type_in_unionall_diffs",

	// Row timestamp column related errors
	527: "rowtimestamp_one_pk_col_only",
	528: "rowtimestamp_pk_col_only",
	529: "rowtimestamp_create_only",
	530: "rowtimestamp_col_invalid_type",
	531: "rowtimestamp_not_allowed_on_view",
	532: "invalid_scn",

	// Column family related exceptions
	1000: "single_pk_may_not_be_null",
	1001: "column_family_not_found",
	1002: "properties_for_family",
	1003: "primary_key_with_family_name",
	1004: "primary_key_out_of_order",
	1005: "varbinary_in_row_key",
	1006: "not_nullable_column_in_row_key",
	1015: "varbinary_last_pk",
	1023: "nullable_fixed_width_last_pk",
	1036: "cannot_modify_view_pk",
	1037: "base_table_column",

	// Key/value column related errors
	1007: "key_value_not_null",

	// View related errors
	1008: "view_with_table_config",
	1009: "view_with_properties",

	// Table related errors that are not in standard code
	1010: "cannot_mutate_table",
	1011: "unexpected_mutation_code",
	1012: "table_undefined",
	1013: "table_already_exist",

	// Syntax error
	1014: "type_not_supported_for_operator",
	1016: "aggregate_in_group_by",
	1017: "aggregate_in_where",
	1018: "aggregate_with_not_group_by_column",
	1019: "only_aggregate_in_having_clause",
	1020: "upsert_column_numbers_mismatch",

	// Table properties exception
	1021: "invalid_bucket_num",
	1022: "no_splits_on_salted_table",
	1024: "salt_only_on_create_table",
	1025: "set_unsupported_prop_on_alter_table",
	1038: "cannot_add_not_nullable_column",
	1026: "no_mutable_indexes",
	1028: "invalid_index_state_transition",
	1029: "invalid_mutable_index_config",
	1030: "cannot_create_tenant_specific_table",
	1034: "default_column_family_only_on_create_table",
	1040: "insufficient_multi_tenant_columns",
	1041: "tenantid_is_of_wrong_type",
	1045: "view_where_is_constant",
	1046: "cannot_update_view_column",
	1047: "too_many_indexes",
	1048: "no_local_index_on_table_with_immutable_rows",
	1049: "column_family_not_allowed_table_property",
	1050: "column_family_not_allowed_for_ttl",
	1051: "cannot_alter_property",
	1052: "cannot_set_property_for_column_not_added",
	1053: "cannot_set_table_property_add_column",
	1054: "no_local_indexes",
	1055: "unallowed_local_indexes",
	1056: "desc_varbinary_not_supported",
	1057: "no_table_specified_for_wildcard_select",
	1058: "unsupported_group_by_expressions",
	1069: "default_column_family_on_shared_table",
	1070: "only_table_may_be_declared_transactional",
	1071: "tx_may_not_switch_to_non_tx",
	1072: "store_nulls_must_be_true_for_transactional",
	1073: "cannot_start_transaction_with_scn_set",
	1074: "tx_max_versions_must_be_greater_than_one",
	1075: "cannot_specify_scn_for_txn_table",
	1076: "null_transaction_context",
	1077: "transaction_failed",
	1078: "cannot_create_txn_table_if_txns_disabled",
	1079: "cannot_alter_to_be_txn_if_txns_disabled",
	1080: "cannot_create_txn_table_with_row_timestamp",
	1081: "cannot_alter_to_be_txn_with_row_timestamp",
	1082: "tx_must_be_enabled_to_set_tx_context",
	1083: "tx_must_be_enabled_to_set_auto_flush",
	1084: "tx_must_be_enabled_to_set_isolation_level",
	1085: "tx_unable_to_get_write_fence",
	1086: "sequence_not_castable_to_auto_partition_id_column",
	1087: "cannot_coerce_auto_partition_id",

	// Sequence related
	1200: "sequence_already_exist",
	1201: "sequence_undefined",
	1202: "start_with_must_be_constant",
	1203: "increment_by_must_be_constant",
	1204: "cache_must_be_non_negative_constant",
	1205: "invalid_use_of_next_value_for",
	1206: "cannot_call_current_before_next_value",
	1207: "empty_sequence_cache",
	1208: "minvalue_must_be_constant",
	1209: "maxvalue_must_be_constant",
	1210: "minvalue_must_be_less_than_or_equal_to_maxvalue",
	1211: "starts_with_must_be_between_min_max_value",
	1212: "sequence_val_reached_max_value",
	1213: "sequence_val_reached_min_value",
	1214: "increment_by_must_not_be_zero",
	1215: "num_seq_to_allocate_must_be_constant",
	1216: "num_seq_to_allocate_not_supported",
	1217: "auto_partition_sequence_undefined",

	// Parser error. (errorcode 06, sqlstate 42p)
	601: "parser_error",
	602: "missing_token",
	603: "unwanted_token",
	604: "mismatched_token",
	605: "unknown_function",

	// Implementation defined class. execution exceptions (errorcode 11, sqlstate xcl)
	1101: "resultset_closed",
	1102: "get_table_regions_fail",
	1103: "execute_query_not_applicable",
	1104: "execute_update_not_applicable",
	1105: "split_point_not_constant",
	1106: "batch_exception",
	1107: "execute_update_with_non_empty_batch",
	1108: "stale_region_boundary_cache",
	1109: "cannot_split_local_index",
	1110: "cannot_salt_local_index",
	1120: "index_failure_block_write",
	1130: "update_cache_frequency_invalid",
	1131: "cannot_drop_col_append_only_schema",
	1132: "view_append_only_schema",

	// Implementation defined class. phoenix internal error. (errorcode 20, sqlstate int)
	2001: "cannot_call_method_on_type",
	2002: "class_not_unwrappable",
	2003: "param_index_out_of_bound",
	2004: "param_value_unbound",
	2005: "interrupted_exception",
	2006: "incompatible_client_server_jar",
	2007: "outdated_jars",
	2008: "index_metadata_not_found",
	2009: "unknown_error_code",
	6000: "operation_timed_out",
	6001: "function_undefined",
	6002: "function_already_exist",
	6003: "unallowed_user_defined_functions",
	721:  "schema_already_exists",
	722:  "schema_not_found",
	723:  "cannot_mutate_schema",
}
