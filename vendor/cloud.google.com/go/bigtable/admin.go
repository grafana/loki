/*
Copyright 2015 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	btapb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	btopt "cloud.google.com/go/bigtable/internal/option"
	"cloud.google.com/go/iam"
	"cloud.google.com/go/internal/optional"
	"cloud.google.com/go/longrunning"
	lroauto "cloud.google.com/go/longrunning/autogen"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"
	field_mask "google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const adminAddr = "bigtableadmin.googleapis.com:443"
const mtlsAdminAddr = "bigtableadmin.mtls.googleapis.com:443"

// ErrPartiallyUnavailable is returned when some locations (clusters) are
// unavailable. Both partial results (retrieved from available locations)
// and the error are returned when this exception occurred.
type ErrPartiallyUnavailable struct {
	Locations []string // unavailable locations
}

func (e ErrPartiallyUnavailable) Error() string {
	return fmt.Sprintf("Unavailable locations: %v", e.Locations)
}

// AdminClient is a client type for performing admin operations within a specific instance.
type AdminClient struct {
	connPool  gtransport.ConnPool
	tClient   btapb.BigtableTableAdminClient
	lroClient *lroauto.OperationsClient

	project, instance string

	// Metadata to be sent with each request.
	md metadata.MD
}

// NewAdminClient creates a new AdminClient for a given project and instance.
func NewAdminClient(ctx context.Context, project, instance string, opts ...option.ClientOption) (*AdminClient, error) {
	o, err := btopt.DefaultClientOptions(adminAddr, mtlsAdminAddr, AdminScope, clientUserAgent)
	if err != nil {
		return nil, err
	}
	// Add gRPC client interceptors to supply Google client information. No external interceptors are passed.
	o = append(o, btopt.ClientInterceptorOptions(nil, nil)...)
	// Need to add scopes for long running operations (for create table & snapshots)
	o = append(o, option.WithScopes(cloudresourcemanager.CloudPlatformScope))
	o = append(o, opts...)
	connPool, err := gtransport.DialPool(ctx, o...)
	if err != nil {
		return nil, fmt.Errorf("dialing: %w", err)
	}

	lroClient, err := lroauto.NewOperationsClient(ctx, gtransport.WithConnPool(connPool))
	if err != nil {
		// This error "should not happen", since we are just reusing old connection
		// and never actually need to dial.
		// If this does happen, we could leak conn. However, we cannot close conn:
		// If the user invoked the function with option.WithGRPCConn,
		// we would close a connection that's still in use.
		// TODO(pongad): investigate error conditions.
		return nil, err
	}

	return &AdminClient{
		connPool:  connPool,
		tClient:   btapb.NewBigtableTableAdminClient(connPool),
		lroClient: lroClient,
		project:   project,
		instance:  instance,
		md:        metadata.Pairs(resourcePrefixHeader, fmt.Sprintf("projects/%s/instances/%s", project, instance)),
	}, nil
}

// Close closes the AdminClient.
func (ac *AdminClient) Close() error {
	return ac.connPool.Close()
}

func (ac *AdminClient) instancePrefix() string {
	return instancePrefix(ac.project, ac.instance)
}

func instancePrefix(project, instance string) string {
	return fmt.Sprintf("projects/%s/instances/%s", project, instance)
}

func (ac *AdminClient) backupPath(cluster, instance, backup string) string {
	return fmt.Sprintf("projects/%s/instances/%s/clusters/%s/backups/%s", ac.project, instance, cluster, backup)
}

func (ac *AdminClient) authorizedViewPath(table, authorizedView string) string {
	return fmt.Sprintf("%s/tables/%s/authorizedViews/%s", ac.instancePrefix(), table, authorizedView)
}

// EncryptionInfo represents the encryption info of a table.
type EncryptionInfo struct {
	Status        *Status
	Type          EncryptionType
	KMSKeyVersion string
}

func newEncryptionInfo(pbInfo *btapb.EncryptionInfo) *EncryptionInfo {
	return &EncryptionInfo{
		Status:        pbInfo.EncryptionStatus,
		Type:          EncryptionType(pbInfo.EncryptionType.Number()),
		KMSKeyVersion: pbInfo.KmsKeyVersion,
	}
}

// Status references google.golang.org/grpc/status.
// It represents an RPC status code, message, and details of EncryptionInfo.
// https://pkg.go.dev/google.golang.org/grpc/internal/status
type Status = status.Status

// EncryptionType is the type of encryption for an instance.
type EncryptionType int32

const (
	// EncryptionTypeUnspecified is the type was not specified, though data at rest remains encrypted.
	EncryptionTypeUnspecified EncryptionType = iota
	// GoogleDefaultEncryption represents that data backing this resource is
	// encrypted at rest with a key that is fully managed by Google. No key
	// version or status will be populated. This is the default state.
	GoogleDefaultEncryption
	// CustomerManagedEncryption represents that data backing this resource is
	// encrypted at rest with a key that is managed by the customer.
	// The in-use version of the key and its status are populated for
	// CMEK-protected tables.
	// CMEK-protected backups are pinned to the key version that was in use at
	// the time the backup was taken. This key version is populated but its
	// status is not tracked and is reported as `UNKNOWN`.
	CustomerManagedEncryption
)

// EncryptionInfoByCluster is a map of cluster name to EncryptionInfo
type EncryptionInfoByCluster map[string][]*EncryptionInfo

// EncryptionInfo gets the current encryption info for the table across all of the clusters.
// The returned map will be keyed by cluster id and contain a status for all of the keys in use.
func (ac *AdminClient) EncryptionInfo(ctx context.Context, table string) (EncryptionInfoByCluster, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)

	res, err := ac.getTable(ctx, table, btapb.Table_ENCRYPTION_VIEW)
	if err != nil {
		return nil, err
	}
	encryptionInfo := EncryptionInfoByCluster{}
	for key, cs := range res.ClusterStates {
		for _, pbInfo := range cs.EncryptionInfo {
			info := EncryptionInfo{}
			info.Status = pbInfo.EncryptionStatus
			info.Type = EncryptionType(pbInfo.EncryptionType.Number())
			info.KMSKeyVersion = pbInfo.KmsKeyVersion
			encryptionInfo[key] = append(encryptionInfo[key], &info)
		}
	}

	return encryptionInfo, nil
}

// Tables returns a list of the tables in the instance.
func (ac *AdminClient) Tables(ctx context.Context) ([]string, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.ListTablesRequest{
		Parent: prefix,
	}

	var res *btapb.ListTablesResponse
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = ac.tClient.ListTables(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(res.Tables))
	for _, tbl := range res.Tables {
		names = append(names, strings.TrimPrefix(tbl.Name, prefix+"/tables/"))
	}
	return names, nil
}

// ChangeStreamRetention indicates how long bigtable should retain change data.
// Minimum is 1 day. Maximum is 7. nil to not change the retention period. 0 to
// disable change stream retention.
type ChangeStreamRetention optional.Duration

// DeletionProtection indicates whether the table is protected against data loss
// i.e. when set to protected, deleting the table, the column families in the table,
// and the instance containing the table would be prohibited.
type DeletionProtection int

// None indicates that deletion protection is unset
// Protected indicates that deletion protection is enabled
// Unprotected indicates that deletion protection is disabled
const (
	None DeletionProtection = iota
	Protected
	Unprotected
)

// TableAutomatedBackupConfig generalizes automated backup configurations.
// Currently, the only supported type of automated backup configuration
// is TableAutomatedBackupPolicy.
type TableAutomatedBackupConfig interface {
	isTableAutomatedBackupConfig()
}

// TableAutomatedBackupPolicy defines an automated backup policy for a table.
// Use nil TableAutomatedBackupPolicy to disable Automated Backups on a table.
// Use nil for a specific field to ignore that field when updating the policy on a table.
type TableAutomatedBackupPolicy struct {
	// How long the automated backups should be retained. The only
	// supported value at this time is 3 days.
	RetentionPeriod optional.Duration
	// How frequently automated backups should occur. The only
	// supported value at this time is 24 hours.
	Frequency optional.Duration
}

func (*TableAutomatedBackupPolicy) isTableAutomatedBackupConfig() {}

func toAutomatedBackupConfigProto(automatedBackupConfig TableAutomatedBackupConfig) (*btapb.Table_AutomatedBackupPolicy_, error) {
	if automatedBackupConfig == nil {
		return nil, nil
	}
	switch backupConfig := automatedBackupConfig.(type) {
	case *TableAutomatedBackupPolicy:
		return backupConfig.toProto()
	default:
		return nil, fmt.Errorf("error: Unknown type of automated backup configuration")
	}
}

func (abp *TableAutomatedBackupPolicy) toProto() (*btapb.Table_AutomatedBackupPolicy_, error) {
	pbAutomatedBackupPolicy := &btapb.Table_AutomatedBackupPolicy{
		RetentionPeriod: durationpb.New(0),
		Frequency:       durationpb.New(0),
	}
	if abp.RetentionPeriod == nil && abp.Frequency == nil {
		return nil, errors.New("at least one of RetentionPeriod and Frequency must be set")
	}
	if abp.RetentionPeriod != nil {
		pbAutomatedBackupPolicy.RetentionPeriod = durationpb.New(optional.ToDuration(abp.RetentionPeriod))
	}
	if abp.Frequency != nil {
		pbAutomatedBackupPolicy.Frequency = durationpb.New(optional.ToDuration(abp.Frequency))
	}
	return &btapb.Table_AutomatedBackupPolicy_{
		AutomatedBackupPolicy: pbAutomatedBackupPolicy,
	}, nil
}

// Family represents a column family with its optional GC policy and value type.
type Family struct {
	GCPolicy  GCPolicy
	ValueType Type
}

// UpdateTableConf is unused
type UpdateTableConf struct{}

// TableConf contains all the information necessary to create a table with column families.
type TableConf struct {
	TableID   string
	SplitKeys []string
	// DEPRECATED: Use ColumnFamilies instead.
	// Families is a map from family name to GCPolicy.
	// Only one of Families or ColumnFamilies may be set.
	Families map[string]GCPolicy
	// ColumnFamilies is a map from family name to family configuration.
	// Only one of Families or ColumnFamilies may be set.
	ColumnFamilies map[string]Family
	// DeletionProtection can be none, protected or unprotected
	// set to protected to make the table protected against data loss
	DeletionProtection    DeletionProtection
	ChangeStreamRetention ChangeStreamRetention
	// Configure an automated backup policy for the table
	AutomatedBackupConfig TableAutomatedBackupConfig
}

// CreateTable creates a new table in the instance.
// This method may return before the table's creation is complete.
func (ac *AdminClient) CreateTable(ctx context.Context, table string) error {
	return ac.CreateTableFromConf(ctx, &TableConf{TableID: table, ChangeStreamRetention: nil, DeletionProtection: None})
}

// CreatePresplitTable creates a new table in the instance.
// The list of row keys will be used to initially split the table into multiple tablets.
// Given two split keys, "s1" and "s2", three tablets will be created,
// spanning the key ranges: [, s1), [s1, s2), [s2, ).
// This method may return before the table's creation is complete.
func (ac *AdminClient) CreatePresplitTable(ctx context.Context, table string, splitKeys []string) error {
	return ac.CreateTableFromConf(ctx, &TableConf{TableID: table, SplitKeys: splitKeys, ChangeStreamRetention: nil, DeletionProtection: None})
}

// CreateTableFromConf creates a new table in the instance from the given configuration.
func (ac *AdminClient) CreateTableFromConf(ctx context.Context, conf *TableConf) error {
	if conf.TableID == "" {
		return errors.New("TableID is required")
	}
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	var reqSplits []*btapb.CreateTableRequest_Split
	for _, split := range conf.SplitKeys {
		reqSplits = append(reqSplits, &btapb.CreateTableRequest_Split{Key: []byte(split)})
	}
	var tbl btapb.Table
	// we'd rather not set anything explicitly if users don't specify a value and let the server set the default value.
	// if DeletionProtection is not set, currently the API will default it to false.
	if conf.DeletionProtection == Protected {
		tbl.DeletionProtection = true
	} else if conf.DeletionProtection == Unprotected {
		tbl.DeletionProtection = false
	}
	if conf.ChangeStreamRetention != nil && conf.ChangeStreamRetention.(time.Duration) != 0 {
		tbl.ChangeStreamConfig = &btapb.ChangeStreamConfig{}
		tbl.ChangeStreamConfig.RetentionPeriod = durationpb.New(conf.ChangeStreamRetention.(time.Duration))
	}

	if conf.AutomatedBackupConfig != nil {
		proto, err := toAutomatedBackupConfigProto(conf.AutomatedBackupConfig)
		if err != nil {
			return err
		}
		tbl.AutomatedBackupConfig = proto
	}

	if conf.Families != nil && conf.ColumnFamilies != nil {
		return errors.New("only one of Families or ColumnFamilies may be set, not both")
	}

	if conf.ColumnFamilies != nil {
		tbl.ColumnFamilies = make(map[string]*btapb.ColumnFamily)
		for fam, config := range conf.ColumnFamilies {
			var gcPolicy *btapb.GcRule
			if config.GCPolicy != nil {
				gcPolicy = config.GCPolicy.proto()
			} else {
				gcPolicy = &btapb.GcRule{}
			}

			var typeProto *btapb.Type = nil
			if config.ValueType != nil {
				typeProto = config.ValueType.proto()
			}

			tbl.ColumnFamilies[fam] = &btapb.ColumnFamily{GcRule: gcPolicy, ValueType: typeProto}
		}
	} else if conf.Families != nil {
		tbl.ColumnFamilies = make(map[string]*btapb.ColumnFamily)
		for fam, policy := range conf.Families {
			tbl.ColumnFamilies[fam] = &btapb.ColumnFamily{GcRule: policy.proto()}
		}
	}
	prefix := ac.instancePrefix()
	req := &btapb.CreateTableRequest{
		Parent:        prefix,
		TableId:       conf.TableID,
		Table:         &tbl,
		InitialSplits: reqSplits,
	}
	_, err := ac.tClient.CreateTable(ctx, req)
	return err
}

// CreateColumnFamily creates a new column family in a table.
func (ac *AdminClient) CreateColumnFamily(ctx context.Context, table, family string) error {
	// TODO(dsymonds): Permit specifying gcexpr and any other family settings.
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.ModifyColumnFamiliesRequest{
		Name: prefix + "/tables/" + table,
		Modifications: []*btapb.ModifyColumnFamiliesRequest_Modification{{
			Id:  family,
			Mod: &btapb.ModifyColumnFamiliesRequest_Modification_Create{Create: &btapb.ColumnFamily{}},
		}},
	}
	_, err := ac.tClient.ModifyColumnFamilies(ctx, req)
	return err
}

// CreateColumnFamilyWithConfig creates a new column family in a table with an optional GC policy and value type.
func (ac *AdminClient) CreateColumnFamilyWithConfig(ctx context.Context, table, family string, config Family) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()

	cf := &btapb.ColumnFamily{}
	if config.GCPolicy != nil {
		cf.GcRule = config.GCPolicy.proto()
	}
	if config.ValueType != nil {
		cf.ValueType = config.ValueType.proto()
	}

	req := &btapb.ModifyColumnFamiliesRequest{
		Name: prefix + "/tables/" + table,
		Modifications: []*btapb.ModifyColumnFamiliesRequest_Modification{{
			Id:  family,
			Mod: &btapb.ModifyColumnFamiliesRequest_Modification_Create{Create: cf},
		}},
	}
	_, err := ac.tClient.ModifyColumnFamilies(ctx, req)
	return err
}

const (
	deletionProtectionFieldMask    = "deletion_protection"
	changeStreamConfigFieldMask    = "change_stream_config"
	automatedBackupPolicyFieldMask = "automated_backup_policy"
	retentionPeriodFieldMaskPath   = "retention_period"
	frequencyFieldMaskPath         = "frequency"
)

func (ac *AdminClient) newUpdateTableRequestProto(tableID string) (*btapb.UpdateTableRequest, error) {
	if tableID == "" {
		return nil, errors.New("TableID is required")
	}
	updateMask := &field_mask.FieldMask{
		Paths: []string{},
	}
	req := &btapb.UpdateTableRequest{
		Table: &btapb.Table{
			Name: ac.instancePrefix() + "/tables/" + tableID,
		},
		UpdateMask: updateMask,
	}
	return req, nil
}

func (ac *AdminClient) updateTableAndWait(ctx context.Context, updateTableRequest *btapb.UpdateTableRequest) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)

	lro, err := ac.tClient.UpdateTable(ctx, updateTableRequest)
	if err != nil {
		return fmt.Errorf("error from update: %w", err)
	}

	var tbl btapb.Table
	op := longrunning.InternalNewOperation(ac.lroClient, lro)
	err = op.Wait(ctx, &tbl)
	if err != nil {
		return fmt.Errorf("error from operation: %v", err)
	}

	return nil
}

// UpdateTableDisableChangeStream updates a table to disable change stream for table ID.
func (ac *AdminClient) UpdateTableDisableChangeStream(ctx context.Context, tableID string) error {
	req, err := ac.newUpdateTableRequestProto(tableID)
	if err != nil {
		return err
	}
	req.UpdateMask.Paths = []string{changeStreamConfigFieldMask}
	return ac.updateTableAndWait(ctx, req)
}

// UpdateTableWithChangeStream updates a table to with the given table ID and change stream config.
func (ac *AdminClient) UpdateTableWithChangeStream(ctx context.Context, tableID string, changeStreamRetention ChangeStreamRetention) error {
	req, err := ac.newUpdateTableRequestProto(tableID)
	if err != nil {
		return err
	}
	req.UpdateMask.Paths = []string{changeStreamConfigFieldMask + "." + retentionPeriodFieldMaskPath}
	req.Table.ChangeStreamConfig = &btapb.ChangeStreamConfig{}
	req.Table.ChangeStreamConfig.RetentionPeriod = durationpb.New(changeStreamRetention.(time.Duration))
	return ac.updateTableAndWait(ctx, req)
}

// UpdateTableWithDeletionProtection updates a table with the given table ID and deletion protection parameter.
func (ac *AdminClient) UpdateTableWithDeletionProtection(ctx context.Context, tableID string, deletionProtection DeletionProtection) error {
	req, err := ac.newUpdateTableRequestProto(tableID)
	if err != nil {
		return err
	}
	req.UpdateMask.Paths = []string{deletionProtectionFieldMask}
	req.Table.DeletionProtection = deletionProtection != Unprotected
	return ac.updateTableAndWait(ctx, req)
}

// UpdateTableDisableAutomatedBackupPolicy updates a table to disable automated backups for table ID.
func (ac *AdminClient) UpdateTableDisableAutomatedBackupPolicy(ctx context.Context, tableID string) error {
	req, err := ac.newUpdateTableRequestProto(tableID)
	if err != nil {
		return err
	}
	req.UpdateMask.Paths = []string{automatedBackupPolicyFieldMask}
	return ac.updateTableAndWait(ctx, req)
}

// UpdateTableWithAutomatedBackupPolicy updates a table to with the given table ID and automated backup policy config.
func (ac *AdminClient) UpdateTableWithAutomatedBackupPolicy(ctx context.Context, tableID string, automatedBackupPolicy TableAutomatedBackupPolicy) error {
	req, err := ac.newUpdateTableRequestProto(tableID)
	if err != nil {
		return err
	}
	abc, err := toAutomatedBackupConfigProto(&automatedBackupPolicy)
	if err != nil {
		return err
	}
	// If the AutomatedBackupPolicy is not at least partially specified, or if both fields are 0, then this is an
	// incorrect configuration for updating the table, and should be rejected. Both fields could be zero if (1)
	// they are set to zero, or (2) neither field was set and the policy was constructed using toProto().
	if abc.AutomatedBackupPolicy.RetentionPeriod.Seconds == 0 && abc.AutomatedBackupPolicy.Frequency.Seconds == 0 {
		return errors.New("Invalid automated backup policy. If you're intending to disable automated backups, please use the UpdateTableDisableAutomatedBackupPolicy method instead")
	}
	if abc.AutomatedBackupPolicy.RetentionPeriod.Seconds != 0 {
		// Update Retention Period
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, automatedBackupPolicyFieldMask+"."+retentionPeriodFieldMaskPath)
	}
	if abc.AutomatedBackupPolicy.Frequency.Seconds != 0 {
		// Update Frequency
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, automatedBackupPolicyFieldMask+"."+frequencyFieldMaskPath)
	}
	req.Table.AutomatedBackupConfig = abc
	return ac.updateTableAndWait(ctx, req)
}

// DeleteTable deletes a table and all of its data.
func (ac *AdminClient) DeleteTable(ctx context.Context, table string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.DeleteTableRequest{
		Name: prefix + "/tables/" + table,
	}
	_, err := ac.tClient.DeleteTable(ctx, req)
	return err
}

// DeleteColumnFamily deletes a column family in a table and all of its data.
func (ac *AdminClient) DeleteColumnFamily(ctx context.Context, table, family string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.ModifyColumnFamiliesRequest{
		Name: prefix + "/tables/" + table,
		Modifications: []*btapb.ModifyColumnFamiliesRequest_Modification{{
			Id:  family,
			Mod: &btapb.ModifyColumnFamiliesRequest_Modification_Drop{Drop: true},
		}},
	}
	_, err := ac.tClient.ModifyColumnFamilies(ctx, req)
	return err
}

// TableInfo represents information about a table.
type TableInfo struct {
	// DEPRECATED - This field is deprecated. Please use FamilyInfos instead.
	Families    []string
	FamilyInfos []FamilyInfo
	// DeletionProtection indicates whether the table is protected against data loss
	// DeletionProtection could be None depending on the table view
	// for example when using NAME_ONLY, the response does not contain DeletionProtection and the value should be None
	DeletionProtection    DeletionProtection
	ChangeStreamRetention ChangeStreamRetention
	AutomatedBackupConfig TableAutomatedBackupConfig
}

// FamilyInfo represents information about a column family.
type FamilyInfo struct {
	Name         string
	GCPolicy     string
	FullGCPolicy GCPolicy
	ValueType    Type
}

func (ac *AdminClient) getTable(ctx context.Context, table string, view btapb.Table_View) (*btapb.Table, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.GetTableRequest{
		Name: prefix + "/tables/" + table,
		View: view,
	}

	var res *btapb.Table

	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = ac.tClient.GetTable(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// TableInfo retrieves information about a table.
func (ac *AdminClient) TableInfo(ctx context.Context, table string) (*TableInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)

	res, err := ac.getTable(ctx, table, btapb.Table_SCHEMA_VIEW)
	if err != nil {
		return nil, err
	}

	ti := &TableInfo{}
	for name, fam := range res.ColumnFamilies {
		ti.Families = append(ti.Families, name)
		ti.FamilyInfos = append(ti.FamilyInfos, FamilyInfo{
			Name:         name,
			GCPolicy:     GCRuleToString(fam.GcRule),
			FullGCPolicy: gcRuleToPolicy(fam.GcRule),
			ValueType:    ProtoToType(fam.ValueType),
		})
	}
	// we expect DeletionProtection to be in the response because Table_SCHEMA_VIEW is being used in this function
	// but when using NAME_ONLY, the response does not contain DeletionProtection and it could be nil
	if res.DeletionProtection == true {
		ti.DeletionProtection = Protected
	} else {
		ti.DeletionProtection = Unprotected
	}
	if res.ChangeStreamConfig != nil && res.ChangeStreamConfig.RetentionPeriod != nil {
		ti.ChangeStreamRetention = res.ChangeStreamConfig.RetentionPeriod.AsDuration()
	}
	if res.AutomatedBackupConfig != nil {
		switch res.AutomatedBackupConfig.(type) {
		case *btapb.Table_AutomatedBackupPolicy_:
			ti.AutomatedBackupConfig = &TableAutomatedBackupPolicy{
				RetentionPeriod: res.GetAutomatedBackupPolicy().GetRetentionPeriod().AsDuration(),
				Frequency:       res.GetAutomatedBackupPolicy().GetFrequency().AsDuration(),
			}
		default:
			return nil, fmt.Errorf("error: Unknown type of automated backup configuration")
		}
	}

	return ti, nil
}

type gcPolicySettings struct {
	ignoreWarnings bool
}

// GCPolicyOption is the interface to change GC policy settings
type GCPolicyOption interface {
	apply(s *gcPolicySettings)
}

type ignoreWarnings bool

func (w ignoreWarnings) apply(s *gcPolicySettings) {
	s.ignoreWarnings = bool(w)
}

// IgnoreWarnings returns a gcPolicyOption that ignores safety checks when modifying the column families
func IgnoreWarnings() GCPolicyOption {
	return ignoreWarnings(true)
}

func (ac *AdminClient) setGCPolicy(ctx context.Context, table, family string, policy GCPolicy, opts ...GCPolicyOption) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()

	s := gcPolicySettings{}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(&s)
		}
	}
	req := &btapb.ModifyColumnFamiliesRequest{
		Name: prefix + "/tables/" + table,
		Modifications: []*btapb.ModifyColumnFamiliesRequest_Modification{{
			Id:  family,
			Mod: &btapb.ModifyColumnFamiliesRequest_Modification_Update{Update: &btapb.ColumnFamily{GcRule: policy.proto()}},
		}},
		IgnoreWarnings: s.ignoreWarnings,
	}
	_, err := ac.tClient.ModifyColumnFamilies(ctx, req)
	return err
}

// SetGCPolicy specifies which cells in a column family should be garbage collected.
// GC executes opportunistically in the background; table reads may return data
// matching the GC policy.
func (ac *AdminClient) SetGCPolicy(ctx context.Context, table, family string, policy GCPolicy) error {
	return ac.SetGCPolicyWithOptions(ctx, table, family, policy)
}

// SetGCPolicyWithOptions is similar to SetGCPolicy but allows passing options
func (ac *AdminClient) SetGCPolicyWithOptions(ctx context.Context, table, family string, policy GCPolicy, opts ...GCPolicyOption) error {
	return ac.setGCPolicy(ctx, table, family, policy, opts...)
}

// DropRowRange permanently deletes a row range from the specified table.
func (ac *AdminClient) DropRowRange(ctx context.Context, table, rowKeyPrefix string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.DropRowRangeRequest{
		Name:   prefix + "/tables/" + table,
		Target: &btapb.DropRowRangeRequest_RowKeyPrefix{RowKeyPrefix: []byte(rowKeyPrefix)},
	}
	_, err := ac.tClient.DropRowRange(ctx, req)
	return err
}

// DropAllRows permanently deletes all rows from the specified table.
func (ac *AdminClient) DropAllRows(ctx context.Context, table string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	req := &btapb.DropRowRangeRequest{
		Name:   prefix + "/tables/" + table,
		Target: &btapb.DropRowRangeRequest_DeleteAllDataFromTable{DeleteAllDataFromTable: true},
	}
	_, err := ac.tClient.DropRowRange(ctx, req)
	return err
}

// CreateTableFromSnapshot creates a table from snapshot.
// The table will be created in the same cluster as the snapshot.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature
// is not currently available to most Cloud Bigtable customers. This feature
// might be changed in backward-incompatible ways and is not recommended for
// production use. It is not subject to any SLA or deprecation policy.
func (ac *AdminClient) CreateTableFromSnapshot(ctx context.Context, table, cluster, snapshot string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	snapshotPath := prefix + "/clusters/" + cluster + "/snapshots/" + snapshot

	req := &btapb.CreateTableFromSnapshotRequest{
		Parent:         prefix,
		TableId:        table,
		SourceSnapshot: snapshotPath,
	}
	op, err := ac.tClient.CreateTableFromSnapshot(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Table{}
	return longrunning.InternalNewOperation(ac.lroClient, op).Wait(ctx, &resp)
}

// DefaultSnapshotDuration is the default TTL for a snapshot.
const DefaultSnapshotDuration time.Duration = 0

// SnapshotTable creates a new snapshot in the specified cluster from the
// specified source table. Setting the TTL to `DefaultSnapshotDuration` will
// use the server side default for the duration.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature
// is not currently available to most Cloud Bigtable customers. This feature
// might be changed in backward-incompatible ways and is not recommended for
// production use. It is not subject to any SLA or deprecation policy.
func (ac *AdminClient) SnapshotTable(ctx context.Context, table, cluster, snapshot string, ttl time.Duration) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()

	var ttlProto *durationpb.Duration

	if ttl > 0 {
		ttlProto = durationpb.New(ttl)
	}

	req := &btapb.SnapshotTableRequest{
		Name:       prefix + "/tables/" + table,
		Cluster:    prefix + "/clusters/" + cluster,
		SnapshotId: snapshot,
		Ttl:        ttlProto,
	}

	op, err := ac.tClient.SnapshotTable(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Snapshot{}
	return longrunning.InternalNewOperation(ac.lroClient, op).Wait(ctx, &resp)
}

// Snapshots returns a SnapshotIterator for iterating over the snapshots in a cluster.
// To list snapshots across all of the clusters in the instance specify "-" as the cluster.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature is not
// currently available to most Cloud Bigtable customers. This feature might be
// changed in backward-incompatible ways and is not recommended for production use.
// It is not subject to any SLA or deprecation policy.
func (ac *AdminClient) Snapshots(ctx context.Context, cluster string) *SnapshotIterator {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	clusterPath := prefix + "/clusters/" + cluster

	it := &SnapshotIterator{}
	req := &btapb.ListSnapshotsRequest{
		Parent: clusterPath,
	}

	fetch := func(pageSize int, pageToken string) (string, error) {
		req.PageToken = pageToken
		if pageSize > math.MaxInt32 {
			req.PageSize = math.MaxInt32
		} else {
			req.PageSize = int32(pageSize)
		}

		var resp *btapb.ListSnapshotsResponse
		err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
			var err error
			resp, err = ac.tClient.ListSnapshots(ctx, req)
			return err
		}, retryOptions...)
		if err != nil {
			return "", err
		}
		for _, s := range resp.Snapshots {
			snapshotInfo, err := newSnapshotInfo(s)
			if err != nil {
				return "", fmt.Errorf("failed to parse snapshot proto %w", err)
			}
			it.items = append(it.items, snapshotInfo)
		}
		return resp.NextPageToken, nil
	}
	bufLen := func() int { return len(it.items) }
	takeBuf := func() interface{} { b := it.items; it.items = nil; return b }

	it.pageInfo, it.nextFunc = iterator.NewPageInfo(fetch, bufLen, takeBuf)

	return it
}

func newSnapshotInfo(snapshot *btapb.Snapshot) (*SnapshotInfo, error) {
	nameParts := strings.Split(snapshot.Name, "/")
	name := nameParts[len(nameParts)-1]
	tablePathParts := strings.Split(snapshot.SourceTable.Name, "/")
	tableID := tablePathParts[len(tablePathParts)-1]

	if err := snapshot.CreateTime.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid createTime: %w", err)
	}
	createTime := snapshot.GetCreateTime().AsTime()

	if err := snapshot.DeleteTime.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid deleteTime: %v", err)
	}
	deleteTime := snapshot.GetDeleteTime().AsTime()

	return &SnapshotInfo{
		Name:        name,
		SourceTable: tableID,
		DataSize:    snapshot.DataSizeBytes,
		CreateTime:  createTime,
		DeleteTime:  deleteTime,
	}, nil
}

// SnapshotIterator is an EntryIterator that iterates over log entries.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature
// is not currently available to most Cloud Bigtable customers. This feature
// might be changed in backward-incompatible ways and is not recommended for
// production use. It is not subject to any SLA or deprecation policy.
type SnapshotIterator struct {
	items    []*SnapshotInfo
	pageInfo *iterator.PageInfo
	nextFunc func() error
}

// PageInfo supports pagination. See https://godoc.org/google.golang.org/api/iterator package for details.
func (it *SnapshotIterator) PageInfo() *iterator.PageInfo {
	return it.pageInfo
}

// Next returns the next result. Its second return value is iterator.Done
// (https://godoc.org/google.golang.org/api/iterator) if there are no more
// results. Once Next returns Done, all subsequent calls will return Done.
func (it *SnapshotIterator) Next() (*SnapshotInfo, error) {
	if err := it.nextFunc(); err != nil {
		return nil, err
	}
	item := it.items[0]
	it.items = it.items[1:]
	return item, nil
}

// SnapshotInfo contains snapshot metadata.
type SnapshotInfo struct {
	Name        string
	SourceTable string
	DataSize    int64
	CreateTime  time.Time
	DeleteTime  time.Time
}

// SnapshotInfo gets snapshot metadata.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature
// is not currently available to most Cloud Bigtable customers. This feature
// might be changed in backward-incompatible ways and is not recommended for
// production use. It is not subject to any SLA or deprecation policy.
func (ac *AdminClient) SnapshotInfo(ctx context.Context, cluster, snapshot string) (*SnapshotInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	clusterPath := prefix + "/clusters/" + cluster
	snapshotPath := clusterPath + "/snapshots/" + snapshot

	req := &btapb.GetSnapshotRequest{
		Name: snapshotPath,
	}

	var resp *btapb.Snapshot
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		resp, err = ac.tClient.GetSnapshot(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	return newSnapshotInfo(resp)
}

// DeleteSnapshot deletes a snapshot in a cluster.
//
// This is a private alpha release of Cloud Bigtable snapshots. This feature
// is not currently available to most Cloud Bigtable customers. This feature
// might be changed in backward-incompatible ways and is not recommended for
// production use. It is not subject to any SLA or deprecation policy.
func (ac *AdminClient) DeleteSnapshot(ctx context.Context, cluster, snapshot string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	clusterPath := prefix + "/clusters/" + cluster
	snapshotPath := clusterPath + "/snapshots/" + snapshot

	req := &btapb.DeleteSnapshotRequest{
		Name: snapshotPath,
	}
	_, err := ac.tClient.DeleteSnapshot(ctx, req)
	return err
}

// getConsistencyToken gets the consistency token for a table.
func (ac *AdminClient) getConsistencyToken(ctx context.Context, tableName string) (string, error) {
	req := &btapb.GenerateConsistencyTokenRequest{
		Name: tableName,
	}
	resp, err := ac.tClient.GenerateConsistencyToken(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.GetConsistencyToken(), nil
}

// isConsistent checks if a token is consistent for a table.
func (ac *AdminClient) isConsistent(ctx context.Context, tableName, token string) (bool, error) {
	req := &btapb.CheckConsistencyRequest{
		Name:             tableName,
		ConsistencyToken: token,
	}
	var resp *btapb.CheckConsistencyResponse

	// Retry calls on retryable errors to avoid losing the token gathered before.
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		resp, err = ac.tClient.CheckConsistency(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return false, err
	}
	return resp.GetConsistent(), nil
}

// WaitForReplication waits until all the writes committed before the call started have been propagated to all the clusters in the instance via replication.
func (ac *AdminClient) WaitForReplication(ctx context.Context, table string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	// Get the token.
	prefix := ac.instancePrefix()
	tableName := prefix + "/tables/" + table
	token, err := ac.getConsistencyToken(ctx, tableName)
	if err != nil {
		return err
	}

	// Periodically check if the token is consistent.
	timer := time.NewTicker(time.Second * 10)
	defer timer.Stop()
	for {
		consistent, err := ac.isConsistent(ctx, tableName, token)
		if err != nil {
			return err
		}
		if consistent {
			return nil
		}
		// Sleep for a bit or until the ctx is cancelled.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// TableIAM creates an IAM Handle specific to a given Instance and Table within the configured project.
func (ac *AdminClient) TableIAM(tableID string) *iam.Handle {
	return iam.InternalNewHandleGRPCClient(ac.tClient,
		"projects/"+ac.project+"/instances/"+ac.instance+"/tables/"+tableID)
}

// BackupIAM creates an IAM Handle specific to a given Cluster and Backup.
func (ac *AdminClient) BackupIAM(cluster, backup string) *iam.Handle {
	return iam.InternalNewHandleGRPCClient(ac.tClient, ac.backupPath(cluster, ac.instance, backup))
}

// AuthorizedViewIAM creates an IAM Handle specific to a given Table and AuthorizedView.
func (ac *AdminClient) AuthorizedViewIAM(table, authorizedView string) *iam.Handle {
	return iam.InternalNewHandleGRPCClient(ac.tClient, ac.authorizedViewPath(table, authorizedView))
}

const instanceAdminAddr = "bigtableadmin.googleapis.com:443"
const mtlsInstanceAdminAddr = "bigtableadmin.mtls.googleapis.com:443"

// InstanceAdminClient is a client type for performing admin operations on instances.
// These operations can be substantially more dangerous than those provided by AdminClient.
type InstanceAdminClient struct {
	connPool  gtransport.ConnPool
	iClient   btapb.BigtableInstanceAdminClient
	lroClient *lroauto.OperationsClient

	project string

	// Metadata to be sent with each request.
	md metadata.MD
}

// NewInstanceAdminClient creates a new InstanceAdminClient for a given project.
func NewInstanceAdminClient(ctx context.Context, project string, opts ...option.ClientOption) (*InstanceAdminClient, error) {
	o, err := btopt.DefaultClientOptions(instanceAdminAddr, mtlsInstanceAdminAddr, InstanceAdminScope, clientUserAgent)
	if err != nil {
		return nil, err
	}
	// Add gRPC client interceptors to supply Google client information. No external interceptors are passed.
	o = append(o, btopt.ClientInterceptorOptions(nil, nil)...)
	o = append(o, opts...)
	connPool, err := gtransport.DialPool(ctx, o...)
	if err != nil {
		return nil, fmt.Errorf("dialing: %w", err)
	}

	lroClient, err := lroauto.NewOperationsClient(ctx, gtransport.WithConnPool(connPool))
	if err != nil {
		// This error "should not happen", since we are just reusing old connection
		// and never actually need to dial.
		// If this does happen, we could leak conn. However, we cannot close conn:
		// If the user invoked the function with option.WithGRPCConn,
		// we would close a connection that's still in use.
		// TODO(pongad): investigate error conditions.
		return nil, err
	}

	return &InstanceAdminClient{
		connPool:  connPool,
		iClient:   btapb.NewBigtableInstanceAdminClient(connPool),
		lroClient: lroClient,

		project: project,
		md:      metadata.Pairs(resourcePrefixHeader, "projects/"+project),
	}, nil
}

// Close closes the InstanceAdminClient.
func (iac *InstanceAdminClient) Close() error {
	return iac.connPool.Close()
}

// StorageType is the type of storage used for all tables in an instance
type StorageType int

const (
	SSD StorageType = iota
	HDD
)

func (st StorageType) proto() btapb.StorageType {
	if st == HDD {
		return btapb.StorageType_HDD
	}
	return btapb.StorageType_SSD
}

func storageTypeFromProto(st btapb.StorageType) StorageType {
	if st == btapb.StorageType_HDD {
		return HDD
	}

	return SSD
}

// InstanceState is the state of the instance. This is output-only.
type InstanceState int32

const (
	// NotKnown represents the state of an instance that could not be determined.
	NotKnown InstanceState = InstanceState(btapb.Instance_STATE_NOT_KNOWN)
	// Ready represents the state of an instance that has been successfully created.
	Ready = InstanceState(btapb.Instance_READY)
	// Creating represents the state of an instance that is currently being created.
	Creating = InstanceState(btapb.Instance_CREATING)
)

// InstanceType is the type of the instance.
type InstanceType int32

const (
	// UNSPECIFIED instance types default to PRODUCTION
	UNSPECIFIED InstanceType = InstanceType(btapb.Instance_TYPE_UNSPECIFIED)
	PRODUCTION               = InstanceType(btapb.Instance_PRODUCTION)
	DEVELOPMENT              = InstanceType(btapb.Instance_DEVELOPMENT)
)

// InstanceInfo represents information about an instance
type InstanceInfo struct {
	Name          string // name of the instance
	DisplayName   string // display name for UIs
	InstanceState InstanceState
	InstanceType  InstanceType
	Labels        map[string]string
}

// InstanceConf contains the information necessary to create an Instance
type InstanceConf struct {
	InstanceId, DisplayName, ClusterId, Zone string
	// NumNodes must not be specified for DEVELOPMENT instance types
	NumNodes     int32
	StorageType  StorageType
	InstanceType InstanceType
	Labels       map[string]string

	// AutoscalingConfig configures the autoscaling properties on the cluster
	// created with the instance. It is optional.
	AutoscalingConfig *AutoscalingConfig
}

// InstanceWithClustersConfig contains the information necessary to create an Instance
type InstanceWithClustersConfig struct {
	InstanceID, DisplayName string
	Clusters                []ClusterConfig
	InstanceType            InstanceType
	Labels                  map[string]string
}

var instanceNameRegexp = regexp.MustCompile(`^projects/([^/]+)/instances/([a-z][-a-z0-9]*)$`)

// CreateInstance creates a new instance in the project.
// This method will return when the instance has been created or when an error occurs.
func (iac *InstanceAdminClient) CreateInstance(ctx context.Context, conf *InstanceConf) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	newConfig := &InstanceWithClustersConfig{
		InstanceID:   conf.InstanceId,
		DisplayName:  conf.DisplayName,
		InstanceType: conf.InstanceType,
		Labels:       conf.Labels,
		Clusters: []ClusterConfig{
			{
				InstanceID:        conf.InstanceId,
				ClusterID:         conf.ClusterId,
				Zone:              conf.Zone,
				NumNodes:          conf.NumNodes,
				StorageType:       conf.StorageType,
				AutoscalingConfig: conf.AutoscalingConfig,
			},
		},
	}
	return iac.CreateInstanceWithClusters(ctx, newConfig)
}

// CreateInstanceWithClusters creates a new instance with configured clusters in the project.
// This method will return when the instance has been created or when an error occurs.
func (iac *InstanceAdminClient) CreateInstanceWithClusters(ctx context.Context, conf *InstanceWithClustersConfig) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	clusters := make(map[string]*btapb.Cluster)
	for _, cluster := range conf.Clusters {
		clusters[cluster.ClusterID] = cluster.proto(iac.project)
	}

	req := &btapb.CreateInstanceRequest{
		Parent:     "projects/" + iac.project,
		InstanceId: conf.InstanceID,
		Instance: &btapb.Instance{
			DisplayName: conf.DisplayName,
			Type:        btapb.Instance_Type(conf.InstanceType),
			Labels:      conf.Labels,
		},
		Clusters: clusters,
	}

	lro, err := iac.iClient.CreateInstance(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Instance{}
	return longrunning.InternalNewOperation(iac.lroClient, lro).Wait(ctx, &resp)
}

// updateInstance updates a single instance based on config fields that operate
// at an instance level: DisplayName and InstanceType.
func (iac *InstanceAdminClient) updateInstance(ctx context.Context, conf *InstanceWithClustersConfig) (updated bool, err error) {
	if conf.InstanceID == "" {
		return false, errors.New("InstanceID is required")
	}

	// Update the instance, if necessary
	mask := &field_mask.FieldMask{}
	ireq := &btapb.PartialUpdateInstanceRequest{
		Instance: &btapb.Instance{
			Name: "projects/" + iac.project + "/instances/" + conf.InstanceID,
		},
		UpdateMask: mask,
	}
	if conf.DisplayName != "" {
		ireq.Instance.DisplayName = conf.DisplayName
		mask.Paths = append(mask.Paths, "display_name")
	}
	if btapb.Instance_Type(conf.InstanceType) != btapb.Instance_TYPE_UNSPECIFIED {
		ireq.Instance.Type = btapb.Instance_Type(conf.InstanceType)
		mask.Paths = append(mask.Paths, "type")
	}
	if conf.Labels != nil {
		ireq.Instance.Labels = conf.Labels
		mask.Paths = append(mask.Paths, "labels")
	}

	if len(mask.Paths) == 0 {
		return false, nil
	}

	lro, err := iac.iClient.PartialUpdateInstance(ctx, ireq)
	if err != nil {
		return false, err
	}
	err = longrunning.InternalNewOperation(iac.lroClient, lro).Wait(ctx, nil)
	if err != nil {
		return false, err
	}

	return true, nil
}

// UpdateInstanceWithClusters updates an instance and its clusters. Updateable
// fields are instance display name, instance type and cluster size.
// The provided InstanceWithClustersConfig is used as follows:
//   - InstanceID is required
//   - DisplayName and InstanceType are updated only if they are not empty
//   - ClusterID is required for any provided cluster
//   - All other cluster fields are ignored except for NumNodes and
//     AutoscalingConfig, which if set will be updated. If both are provided,
//     AutoscalingConfig takes precedence.
//
// This method may return an error after partially succeeding, for example if the instance is updated
// but a cluster update fails. If an error is returned, InstanceInfo and Clusters may be called to
// determine the current state.
func (iac *InstanceAdminClient) UpdateInstanceWithClusters(ctx context.Context, conf *InstanceWithClustersConfig) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)

	for _, cluster := range conf.Clusters {
		if cluster.ClusterID == "" {
			return errors.New("ClusterID is required for every cluster")
		}
	}

	updatedInstance, err := iac.updateInstance(ctx, conf)
	if err != nil {
		return err
	}

	// Update any clusters
	for _, cluster := range conf.Clusters {
		var clusterErr error
		if cluster.AutoscalingConfig != nil {
			clusterErr = iac.SetAutoscaling(ctx, conf.InstanceID, cluster.ClusterID, *cluster.AutoscalingConfig)
		} else if cluster.NumNodes > 0 {
			clusterErr = iac.UpdateCluster(ctx, conf.InstanceID, cluster.ClusterID, cluster.NumNodes)
		}
		if clusterErr != nil {
			if updatedInstance {
				// We updated the instance, so note that in the error message.
				return fmt.Errorf("UpdateCluster %q failed %w; however UpdateInstance succeeded",
					cluster.ClusterID, clusterErr)
			}
			return clusterErr
		}
	}

	return nil
}

// DeleteInstance deletes an instance from the project.
func (iac *InstanceAdminClient) DeleteInstance(ctx context.Context, instanceID string) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.DeleteInstanceRequest{Name: "projects/" + iac.project + "/instances/" + instanceID}
	_, err := iac.iClient.DeleteInstance(ctx, req)
	return err
}

// Instances returns a list of instances in the project. If any location
// (cluster) is unavailable due to some transient conditions, Instances
// returns partial results and ErrPartiallyUnavailable error with
// unavailable locations list.
func (iac *InstanceAdminClient) Instances(ctx context.Context) ([]*InstanceInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.ListInstancesRequest{
		Parent: "projects/" + iac.project,
	}
	var res *btapb.ListInstancesResponse
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = iac.iClient.ListInstances(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	var is []*InstanceInfo
	for _, i := range res.Instances {
		m := instanceNameRegexp.FindStringSubmatch(i.Name)
		if m == nil {
			return nil, fmt.Errorf("malformed instance name %q", i.Name)
		}
		is = append(is, &InstanceInfo{
			Name:          m[2],
			DisplayName:   i.DisplayName,
			InstanceState: InstanceState(i.State),
			InstanceType:  InstanceType(i.Type),
			Labels:        i.Labels,
		})
	}
	if len(res.FailedLocations) > 0 {
		// Return partial results and an error in
		// case of some locations are unavailable.
		return is, ErrPartiallyUnavailable{res.FailedLocations}
	}
	return is, nil
}

// InstanceInfo returns information about an instance.
func (iac *InstanceAdminClient) InstanceInfo(ctx context.Context, instanceID string) (*InstanceInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.GetInstanceRequest{
		Name: "projects/" + iac.project + "/instances/" + instanceID,
	}
	var res *btapb.Instance
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = iac.iClient.GetInstance(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	m := instanceNameRegexp.FindStringSubmatch(res.Name)
	if m == nil {
		return nil, fmt.Errorf("malformed instance name %q", res.Name)
	}
	return &InstanceInfo{
		Name:          m[2],
		DisplayName:   res.DisplayName,
		InstanceState: InstanceState(res.State),
		InstanceType:  InstanceType(res.Type),
		Labels:        res.Labels,
	}, nil
}

// AutoscalingConfig contains autoscaling configuration for a cluster.
// For details, see https://cloud.google.com/bigtable/docs/autoscaling.
type AutoscalingConfig struct {
	// MinNodes sets the minumum number of nodes in a cluster. MinNodes must
	// be 1 or greater.
	MinNodes int
	// MaxNodes sets the maximum number of nodes in a cluster. MaxNodes must be
	// equal to or greater than MinNodes.
	MaxNodes int
	// CPUTargetPercent sets the CPU utilization target for your cluster's
	// workload.
	CPUTargetPercent int
	// StorageUtilizationPerNode sets the storage usage target, in GB, for
	// each node in a cluster. This number is limited between 2560 (2.5TiB) and
	// 5120 (5TiB) for a SSD cluster and between 8192 (8TiB) and 16384 (16 TiB)
	// for an HDD cluster. If set to zero, the default values are used:
	// 2560 for SSD and 8192 for HDD.
	StorageUtilizationPerNode int
}

func (a *AutoscalingConfig) proto() *btapb.Cluster_ClusterAutoscalingConfig {
	if a == nil {
		return nil
	}
	return &btapb.Cluster_ClusterAutoscalingConfig{
		AutoscalingLimits: &btapb.AutoscalingLimits{
			MinServeNodes: int32(a.MinNodes),
			MaxServeNodes: int32(a.MaxNodes),
		},
		AutoscalingTargets: &btapb.AutoscalingTargets{
			CpuUtilizationPercent:        int32(a.CPUTargetPercent),
			StorageUtilizationGibPerNode: int32(a.StorageUtilizationPerNode),
		},
	}
}

// ClusterConfig contains the information necessary to create a cluster
type ClusterConfig struct {
	// InstanceID specifies the unique name of the instance. Required.
	InstanceID string

	// ClusterID specifies the unique name of the cluster. Required.
	ClusterID string

	// Zone specifies the location where this cluster's nodes and storage reside.
	// For best performance, clients should be located as close as possible to this
	// cluster. Required.
	Zone string

	// NumNodes specifies the number of nodes allocated to this cluster. More
	// nodes enable higher throughput and more consistent performance. One of
	// NumNodes or AutoscalingConfig is required. If both are set,
	// AutoscalingConfig takes precedence.
	NumNodes int32

	// StorageType specifies the type of storage used by this cluster to serve
	// its parent instance's tables, unless explicitly overridden. Required.
	StorageType StorageType

	// KMSKeyName is the name of the KMS customer managed encryption key (CMEK)
	// to use for at-rest encryption of data in this cluster.  If omitted,
	// Google's default encryption will be used. If specified, the requirements
	// for this key are:
	// 1) The Cloud Bigtable service account associated with the
	//    project that contains the cluster must be granted the
	//    ``cloudkms.cryptoKeyEncrypterDecrypter`` role on the
	//    CMEK.
	// 2) Only regional keys can be used and the region of the
	//    CMEK key must match the region of the cluster.
	// 3) All clusters within an instance must use the same CMEK
	//    key.
	// Optional. Immutable.
	KMSKeyName string

	// AutoscalingConfig configures the autoscaling properties on a cluster.
	// One of NumNodes or AutoscalingConfig is required.
	AutoscalingConfig *AutoscalingConfig
}

func (cc *ClusterConfig) proto(project string) *btapb.Cluster {
	cl := &btapb.Cluster{
		ServeNodes:         cc.NumNodes,
		DefaultStorageType: cc.StorageType.proto(),
		Location:           "projects/" + project + "/locations/" + cc.Zone,
		EncryptionConfig: &btapb.Cluster_EncryptionConfig{
			KmsKeyName: cc.KMSKeyName,
		},
	}

	if asc := cc.AutoscalingConfig; asc != nil {
		cl.Config = &btapb.Cluster_ClusterConfig_{
			ClusterConfig: &btapb.Cluster_ClusterConfig{
				ClusterAutoscalingConfig: asc.proto(),
			},
		}
	}
	return cl
}

// ClusterInfo represents information about a cluster.
type ClusterInfo struct {
	// Name is the name of the cluster.
	Name string

	// Zone is the GCP zone of the cluster (e.g. "us-central1-a").
	Zone string

	// ServeNodes is the number of allocated serve nodes.
	ServeNodes int

	// State is the state of the cluster.
	State string

	// StorageType is the storage type of the cluster.
	StorageType StorageType

	// KMSKeyName is the customer managed encryption key for the cluster.
	KMSKeyName string

	// AutoscalingConfig are the configured values for a cluster.
	AutoscalingConfig *AutoscalingConfig
}

// CreateCluster creates a new cluster in an instance.
// This method will return when the cluster has been created or when an error occurs.
func (iac *InstanceAdminClient) CreateCluster(ctx context.Context, conf *ClusterConfig) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)

	req := &btapb.CreateClusterRequest{
		Parent:    "projects/" + iac.project + "/instances/" + conf.InstanceID,
		ClusterId: conf.ClusterID,
		Cluster:   conf.proto(iac.project),
	}

	lro, err := iac.iClient.CreateCluster(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Cluster{}
	return longrunning.InternalNewOperation(iac.lroClient, lro).Wait(ctx, &resp)
}

// DeleteCluster deletes a cluster from an instance.
func (iac *InstanceAdminClient) DeleteCluster(ctx context.Context, instanceID, clusterID string) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.DeleteClusterRequest{Name: "projects/" + iac.project + "/instances/" + instanceID + "/clusters/" + clusterID}
	_, err := iac.iClient.DeleteCluster(ctx, req)
	return err
}

// SetAutoscaling enables autoscaling on a cluster. To remove autoscaling, use
// UpdateCluster. See AutoscalingConfig documentation for deatils.
func (iac *InstanceAdminClient) SetAutoscaling(ctx context.Context, instanceID, clusterID string, conf AutoscalingConfig) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	cluster := &btapb.Cluster{
		Name: "projects/" + iac.project + "/instances/" + instanceID + "/clusters/" + clusterID,
		Config: &btapb.Cluster_ClusterConfig_{
			ClusterConfig: &btapb.Cluster_ClusterConfig{
				ClusterAutoscalingConfig: conf.proto(),
			},
		},
	}
	lro, err := iac.iClient.PartialUpdateCluster(ctx, &btapb.PartialUpdateClusterRequest{
		UpdateMask: &field_mask.FieldMask{
			Paths: []string{"cluster_config.cluster_autoscaling_config"},
		},
		Cluster: cluster,
	})
	if err != nil {
		return err
	}
	return longrunning.InternalNewOperation(iac.lroClient, lro).Wait(ctx, nil)
}

// UpdateCluster updates attributes of a cluster. If Autoscaling is configured
// for the cluster, it will be removed and replaced by the static number of
// serve nodes specified.
func (iac *InstanceAdminClient) UpdateCluster(ctx context.Context, instanceID, clusterID string, serveNodes int32) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	cluster := &btapb.Cluster{
		Name:       "projects/" + iac.project + "/instances/" + instanceID + "/clusters/" + clusterID,
		ServeNodes: serveNodes,
		// Explicitly removing autoscaling config (and including it in the field
		// mask below)
		Config: nil,
	}
	lro, err := iac.iClient.PartialUpdateCluster(ctx, &btapb.PartialUpdateClusterRequest{
		UpdateMask: &field_mask.FieldMask{
			Paths: []string{"serve_nodes", "cluster_config.cluster_autoscaling_config"},
		},
		Cluster: cluster,
	})
	if err != nil {
		return err
	}
	return longrunning.InternalNewOperation(iac.lroClient, lro).Wait(ctx, nil)
}

// Clusters lists the clusters in an instance. If any location
// (cluster) is unavailable due to some transient conditions, Clusters
// returns partial results and ErrPartiallyUnavailable error with
// unavailable locations list.
func (iac *InstanceAdminClient) Clusters(ctx context.Context, instanceID string) ([]*ClusterInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.ListClustersRequest{Parent: "projects/" + iac.project + "/instances/" + instanceID}
	var res *btapb.ListClustersResponse
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = iac.iClient.ListClusters(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	var cis []*ClusterInfo
	for _, c := range res.Clusters {
		nameParts := strings.Split(c.Name, "/")
		locParts := strings.Split(c.Location, "/")
		kmsKeyName := ""
		if c.EncryptionConfig != nil {
			kmsKeyName = c.EncryptionConfig.KmsKeyName
		}
		ci := &ClusterInfo{
			Name:        nameParts[len(nameParts)-1],
			Zone:        locParts[len(locParts)-1],
			ServeNodes:  int(c.ServeNodes),
			State:       c.State.String(),
			StorageType: storageTypeFromProto(c.DefaultStorageType),
			KMSKeyName:  kmsKeyName,
		}
		if cfg := c.GetClusterConfig(); cfg != nil {
			if asc := fromClusterConfigProto(cfg); asc != nil {
				ci.AutoscalingConfig = asc
			}
		}
		cis = append(cis, ci)
	}
	if len(res.FailedLocations) > 0 {
		// Return partial results and an error in
		// case of some locations are unavailable.
		return cis, ErrPartiallyUnavailable{res.FailedLocations}
	}
	return cis, nil
}

// GetCluster fetches a cluster in an instance
func (iac *InstanceAdminClient) GetCluster(ctx context.Context, instanceID, clusterID string) (*ClusterInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	req := &btapb.GetClusterRequest{
		Name: fmt.Sprintf("projects/%s/instances/%s/clusters/%s", iac.project, instanceID, clusterID),
	}
	var c *btapb.Cluster
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		c, err = iac.iClient.GetCluster(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	kmsKeyName := ""
	if c.EncryptionConfig != nil {
		kmsKeyName = c.EncryptionConfig.KmsKeyName
	}
	nameParts := strings.Split(c.Name, "/")
	locParts := strings.Split(c.Location, "/")
	ci := &ClusterInfo{
		Name:        nameParts[len(nameParts)-1],
		Zone:        locParts[len(locParts)-1],
		ServeNodes:  int(c.ServeNodes),
		State:       c.State.String(),
		StorageType: storageTypeFromProto(c.DefaultStorageType),
		KMSKeyName:  kmsKeyName,
	}
	// Use type assertion to handle protobuf oneof type
	if cfg := c.GetClusterConfig(); cfg != nil {
		if asc := fromClusterConfigProto(cfg); asc != nil {
			ci.AutoscalingConfig = asc
		}
	}
	return ci, nil
}

func fromClusterConfigProto(c *btapb.Cluster_ClusterConfig) *AutoscalingConfig {
	if c == nil {
		return nil
	}
	if c.ClusterAutoscalingConfig == nil {
		return nil
	}
	got := c.ClusterAutoscalingConfig
	if got.AutoscalingLimits == nil || got.AutoscalingTargets == nil {
		return nil
	}
	return &AutoscalingConfig{
		MinNodes:                  int(got.AutoscalingLimits.MinServeNodes),
		MaxNodes:                  int(got.AutoscalingLimits.MaxServeNodes),
		CPUTargetPercent:          int(got.AutoscalingTargets.CpuUtilizationPercent),
		StorageUtilizationPerNode: int(got.AutoscalingTargets.StorageUtilizationGibPerNode),
	}
}

// InstanceIAM returns the instance's IAM handle.
func (iac *InstanceAdminClient) InstanceIAM(instanceID string) *iam.Handle {
	return iam.InternalNewHandleGRPCClient(iac.iClient, "projects/"+iac.project+"/instances/"+instanceID)
}

// Routing policies.
const (
	// MultiClusterRouting is a policy that allows read/write requests to be
	// routed to any cluster in the instance. Requests will will fail over to
	// another cluster in the event of transient errors or delays. Choosing
	// this option sacrifices read-your-writes consistency to improve
	// availability.
	MultiClusterRouting = "multi_cluster_routing_use_any"
	// SingleClusterRouting is a policy that unconditionally routes all
	// read/write requests to a specific cluster. This option preserves
	// read-your-writes consistency, but does not improve availability.
	SingleClusterRouting = "single_cluster_routing"
)

// ProfileConf contains the information necessary to create an profile
type ProfileConf struct {
	Name                     string
	ProfileID                string
	InstanceID               string
	Etag                     string
	Description              string
	RoutingPolicy            string
	ClusterID                string
	AllowTransactionalWrites bool

	// If true, warnings are ignored
	IgnoreWarnings bool
}

// ProfileIterator iterates over profiles.
type ProfileIterator struct {
	items    []*btapb.AppProfile
	pageInfo *iterator.PageInfo
	nextFunc func() error
}

// ProfileAttrsToUpdate define addrs to update during an Update call. If unset, no fields will be replaced.
type ProfileAttrsToUpdate struct {
	// If set, updates the description.
	Description optional.String

	//If set, updates the routing policy.
	RoutingPolicy optional.String

	//If RoutingPolicy is updated to SingleClusterRouting, set these fields as well.
	ClusterID                string
	AllowTransactionalWrites bool

	// If true, warnings are ignored
	IgnoreWarnings bool
}

// GetFieldMaskPath returns the field mask path.
func (p *ProfileAttrsToUpdate) GetFieldMaskPath() []string {
	path := make([]string, 0)
	if p.Description != nil {
		path = append(path, "description")
	}

	if p.RoutingPolicy != nil {
		path = append(path, optional.ToString(p.RoutingPolicy))
	}
	return path
}

// PageInfo supports pagination. See https://godoc.org/google.golang.org/api/iterator package for details.
func (it *ProfileIterator) PageInfo() *iterator.PageInfo {
	return it.pageInfo
}

// Next returns the next result. Its second return value is iterator.Done
// (https://godoc.org/google.golang.org/api/iterator) if there are no more
// results. Once Next returns Done, all subsequent calls will return Done.
func (it *ProfileIterator) Next() (*btapb.AppProfile, error) {
	if err := it.nextFunc(); err != nil {
		return nil, err
	}
	item := it.items[0]
	it.items = it.items[1:]
	return item, nil
}

// CreateAppProfile creates an app profile within an instance.
func (iac *InstanceAdminClient) CreateAppProfile(ctx context.Context, profile ProfileConf) (*btapb.AppProfile, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	parent := "projects/" + iac.project + "/instances/" + profile.InstanceID
	appProfile := &btapb.AppProfile{
		Etag:        profile.Etag,
		Description: profile.Description,
	}

	if profile.RoutingPolicy == "" {
		return nil, errors.New("invalid routing policy")
	}

	switch profile.RoutingPolicy {
	case MultiClusterRouting:
		appProfile.RoutingPolicy = &btapb.AppProfile_MultiClusterRoutingUseAny_{
			MultiClusterRoutingUseAny: &btapb.AppProfile_MultiClusterRoutingUseAny{},
		}
	case SingleClusterRouting:
		appProfile.RoutingPolicy = &btapb.AppProfile_SingleClusterRouting_{
			SingleClusterRouting: &btapb.AppProfile_SingleClusterRouting{
				ClusterId:                profile.ClusterID,
				AllowTransactionalWrites: profile.AllowTransactionalWrites,
			},
		}
	default:
		return nil, errors.New("invalid routing policy")
	}

	return iac.iClient.CreateAppProfile(ctx, &btapb.CreateAppProfileRequest{
		Parent:         parent,
		AppProfile:     appProfile,
		AppProfileId:   profile.ProfileID,
		IgnoreWarnings: profile.IgnoreWarnings,
	})
}

// GetAppProfile gets information about an app profile.
func (iac *InstanceAdminClient) GetAppProfile(ctx context.Context, instanceID, name string) (*btapb.AppProfile, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	profileRequest := &btapb.GetAppProfileRequest{
		Name: "projects/" + iac.project + "/instances/" + instanceID + "/appProfiles/" + name,
	}
	var ap *btapb.AppProfile
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		ap, err = iac.iClient.GetAppProfile(ctx, profileRequest)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}
	return ap, err
}

// ListAppProfiles lists information about app profiles in an instance.
func (iac *InstanceAdminClient) ListAppProfiles(ctx context.Context, instanceID string) *ProfileIterator {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	listRequest := &btapb.ListAppProfilesRequest{
		Parent: "projects/" + iac.project + "/instances/" + instanceID,
	}

	pit := &ProfileIterator{}
	fetch := func(pageSize int, pageToken string) (string, error) {
		listRequest.PageToken = pageToken
		var profileRes *btapb.ListAppProfilesResponse
		err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
			var err error
			profileRes, err = iac.iClient.ListAppProfiles(ctx, listRequest)
			return err
		}, retryOptions...)
		if err != nil {
			return "", err
		}

		pit.items = append(pit.items, profileRes.AppProfiles...)
		return profileRes.NextPageToken, nil
	}

	bufLen := func() int { return len(pit.items) }
	takeBuf := func() interface{} { b := pit.items; pit.items = nil; return b }
	pit.pageInfo, pit.nextFunc = iterator.NewPageInfo(fetch, bufLen, takeBuf)
	return pit

}

// UpdateAppProfile updates an app profile within an instance.
// updateAttrs should be set. If unset, all fields will be replaced.
func (iac *InstanceAdminClient) UpdateAppProfile(ctx context.Context, instanceID, profileID string, updateAttrs ProfileAttrsToUpdate) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)

	profile := &btapb.AppProfile{
		Name: "projects/" + iac.project + "/instances/" + instanceID + "/appProfiles/" + profileID,
	}

	if updateAttrs.Description != nil {
		profile.Description = optional.ToString(updateAttrs.Description)
	}
	if updateAttrs.RoutingPolicy != nil {
		switch optional.ToString(updateAttrs.RoutingPolicy) {
		case MultiClusterRouting:
			profile.RoutingPolicy = &btapb.AppProfile_MultiClusterRoutingUseAny_{
				MultiClusterRoutingUseAny: &btapb.AppProfile_MultiClusterRoutingUseAny{},
			}
		case SingleClusterRouting:
			profile.RoutingPolicy = &btapb.AppProfile_SingleClusterRouting_{
				SingleClusterRouting: &btapb.AppProfile_SingleClusterRouting{
					ClusterId:                updateAttrs.ClusterID,
					AllowTransactionalWrites: updateAttrs.AllowTransactionalWrites,
				},
			}
		default:
			return errors.New("invalid routing policy")
		}
	}
	patchRequest := &btapb.UpdateAppProfileRequest{
		AppProfile: profile,
		UpdateMask: &field_mask.FieldMask{
			Paths: updateAttrs.GetFieldMaskPath(),
		},
		IgnoreWarnings: updateAttrs.IgnoreWarnings,
	}
	updateRequest, err := iac.iClient.UpdateAppProfile(ctx, patchRequest)
	if err != nil {
		return err
	}

	return longrunning.InternalNewOperation(iac.lroClient, updateRequest).Wait(ctx, nil)

}

// DeleteAppProfile deletes an app profile from an instance.
func (iac *InstanceAdminClient) DeleteAppProfile(ctx context.Context, instanceID, name string) error {
	ctx = mergeOutgoingMetadata(ctx, iac.md)
	deleteProfileRequest := &btapb.DeleteAppProfileRequest{
		Name:           "projects/" + iac.project + "/instances/" + instanceID + "/appProfiles/" + name,
		IgnoreWarnings: true,
	}
	_, err := iac.iClient.DeleteAppProfile(ctx, deleteProfileRequest)
	return err

}

// UpdateInstanceResults contains information about the
// changes made after invoking UpdateInstanceAndSyncClusters.
type UpdateInstanceResults struct {
	InstanceUpdated bool
	CreatedClusters []string
	DeletedClusters []string
	UpdatedClusters []string
}

func (r *UpdateInstanceResults) String() string {
	return fmt.Sprintf("Instance updated? %v Clusters added:%v Clusters deleted:%v Clusters updated:%v",
		r.InstanceUpdated, r.CreatedClusters, r.DeletedClusters, r.UpdatedClusters)
}

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}

// UpdateInstanceAndSyncClusters updates an instance and its clusters, and will synchronize the
// clusters in the instance with the provided clusters, creating and deleting them as necessary.
// The provided InstanceWithClustersConfig is used as follows:
//   - InstanceID is required
//   - DisplayName and InstanceType are updated only if they are not empty
//   - ClusterID is required for any provided cluster
//   - Any cluster present in conf.Clusters but not part of the instance will be created using CreateCluster
//     and the given ClusterConfig.
//   - Any cluster missing from conf.Clusters but present in the instance will be removed from the instance
//     using DeleteCluster.
//   - Any cluster in conf.Clusters that also exists in the instance will be
//     updated either to contain the provided number of nodes or to use the
//     provided autoscaling config. If both the number of nodes and autoscaling
//     are configured, autoscaling takes precedence. If the number of nodes is zero
//     and autoscaling is not provided in InstanceWithClustersConfig, the cluster
//     is not updated.
//
// This method may return an error after partially succeeding, for example if the instance is updated
// but a cluster update fails. If an error is returned, InstanceInfo and Clusters may be called to
// determine the current state. The return UpdateInstanceResults will describe the work done by the
// method, whether partial or complete.
func UpdateInstanceAndSyncClusters(ctx context.Context, iac *InstanceAdminClient, conf *InstanceWithClustersConfig) (*UpdateInstanceResults, error) {
	ctx = mergeOutgoingMetadata(ctx, iac.md)

	// First fetch the existing clusters so we know what to remove, add or update.
	existingClusters, err := iac.Clusters(ctx, conf.InstanceID)
	if err != nil {
		return nil, err
	}

	updatedInstance, err := iac.updateInstance(ctx, conf)
	if err != nil {
		return nil, err
	}

	results := &UpdateInstanceResults{InstanceUpdated: updatedInstance}

	existingClusterNames := make(map[string]bool)
	for _, cluster := range existingClusters {
		existingClusterNames[cluster.Name] = true
	}

	// Synchronize clusters that were passed in with the existing clusters in the instance.
	// First update any cluster we encounter that already exists in the instance.
	// Collect the clusters that we will create and delete so that we can minimize disruption
	// of the instance.
	clustersToCreate := list.New()
	clustersToDelete := list.New()
	for _, cluster := range conf.Clusters {
		_, clusterExists := existingClusterNames[cluster.ClusterID]
		if !clusterExists {
			// The cluster doesn't exist yet, so we must create it.
			clustersToCreate.PushBack(cluster)
			continue
		}
		delete(existingClusterNames, cluster.ClusterID)

		if cluster.NumNodes <= 0 && cluster.AutoscalingConfig == nil {
			// We only synchronize clusters with a valid number of nodes
			// or a valid autoscaling config.
			continue
		}

		// We update teh clusters autoscaling config, or its number of serve
		// nodes.
		var updateErr error
		if cluster.AutoscalingConfig != nil {
			updateErr = iac.SetAutoscaling(ctx, conf.InstanceID, cluster.ClusterID,
				*cluster.AutoscalingConfig)
		} else {
			updateErr = iac.UpdateCluster(ctx, conf.InstanceID, cluster.ClusterID,
				cluster.NumNodes)
		}
		if updateErr != nil {
			return results, fmt.Errorf("UpdateCluster %q failed %w; Progress: %v",
				cluster.ClusterID, updateErr, results)
		}
		results.UpdatedClusters = append(results.UpdatedClusters, cluster.ClusterID)
	}

	// Any cluster left in existingClusterNames was NOT in the given config and should be deleted.
	for clusterToDelete := range existingClusterNames {
		clustersToDelete.PushBack(clusterToDelete)
	}

	// Now that we have the clusters that we need to create and delete, we do so keeping the following
	// in mind:
	// - Don't delete the last cluster in the instance, as that will result in an error.
	// - Attempt to offset each deletion with a creation before another deletion, so that instance
	//   capacity is never reduced more than necessary.
	// Note that there is a limit on number of clusters in an instance which we are not aware of here,
	// so delete a cluster before adding one (as long as there are > 1 clusters left) so that we are
	// less likely to exceed the maximum number of clusters.
	numExistingClusters := len(existingClusters)
	nextCreation := clustersToCreate.Front()
	nextDeletion := clustersToDelete.Front()
	for {
		// We are done when both lists are empty.
		if nextCreation == nil && nextDeletion == nil {
			break
		}

		// If there is more than one existing cluster, we always want to delete first if possible.
		// If there are no more creations left, always go ahead with the deletion.
		if (numExistingClusters > 1 && nextDeletion != nil) || nextCreation == nil {
			clusterToDelete := nextDeletion.Value.(string)
			err = iac.DeleteCluster(ctx, conf.InstanceID, clusterToDelete)
			if err != nil {
				return results, fmt.Errorf("DeleteCluster %q failed %w; Progress: %v",
					clusterToDelete, err, results)
			}
			results.DeletedClusters = append(results.DeletedClusters, clusterToDelete)
			numExistingClusters--
			nextDeletion = nextDeletion.Next()
		}

		// Now create a new cluster if required.
		if nextCreation != nil {
			clusterToCreate := nextCreation.Value.(ClusterConfig)
			// Assume the cluster config is well formed and rely on the underlying call to error out.
			// Make sure to set the InstanceID, though, since we know what it must be.
			clusterToCreate.InstanceID = conf.InstanceID
			err = iac.CreateCluster(ctx, &clusterToCreate)
			if err != nil {
				return results, fmt.Errorf("CreateCluster %v failed %w; Progress: %v",
					clusterToCreate, err, results)
			}
			results.CreatedClusters = append(results.CreatedClusters, clusterToCreate.ClusterID)
			numExistingClusters++
			nextCreation = nextCreation.Next()
		}
	}

	return results, nil
}

// RestoreTable creates a table from a backup. The table will be created in the same cluster as the backup.
// To restore a table to a different instance, see RestoreTableFrom.
func (ac *AdminClient) RestoreTable(ctx context.Context, table, cluster, backup string) error {
	return ac.RestoreTableFrom(ctx, ac.instance, table, cluster, backup)
}

// RestoreTableFrom creates a new table in the admin's instance by restoring from the given backup and instance.
// To restore within the same instance, see RestoreTable.
// sourceInstance (ex. "my-instance") and sourceCluster (ex. "my-cluster") are the instance and cluster in which the new table will be restored from.
// tableName (ex. "my-restored-table") will be the name of the newly created table.
// backupName (ex. "my-backup") is the name of the backup to restore.
func (ac *AdminClient) RestoreTableFrom(ctx context.Context, sourceInstance, table, sourceCluster, backup string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	parent := ac.instancePrefix()
	sourceBackupPath := ac.backupPath(sourceCluster, sourceInstance, backup)
	req := &btapb.RestoreTableRequest{
		Parent:  parent,
		TableId: table,
		Source:  &btapb.RestoreTableRequest_Backup{Backup: sourceBackupPath},
	}
	op, err := ac.tClient.RestoreTable(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Table{}
	return longrunning.InternalNewOperation(ac.lroClient, op).Wait(ctx, &resp)
}

// CreateBackup creates a new backup in the specified cluster from the
// specified source table with the user-provided expire time.
func (ac *AdminClient) CreateBackup(ctx context.Context, table, cluster, backup string, expireTime time.Time) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()

	parsedExpireTime := timestamppb.New(expireTime)

	req := &btapb.CreateBackupRequest{
		Parent:   prefix + "/clusters/" + cluster,
		BackupId: backup,
		Backup: &btapb.Backup{
			ExpireTime:  parsedExpireTime,
			SourceTable: prefix + "/tables/" + table,
		},
	}

	op, err := ac.tClient.CreateBackup(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Backup{}
	return longrunning.InternalNewOperation(ac.lroClient, op).Wait(ctx, &resp)
}

// CopyBackup copies the specified source backup with the user-provided expire time.
func (ac *AdminClient) CopyBackup(ctx context.Context, sourceCluster, sourceBackup,
	destProject, destInstance, destCluster, destBackup string, expireTime time.Time) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	sourceBackupPath := ac.backupPath(sourceCluster, ac.instance, sourceBackup)
	destPrefix := instancePrefix(destProject, destInstance)
	req := &btapb.CopyBackupRequest{
		Parent:       destPrefix + "/clusters/" + destCluster,
		BackupId:     destBackup,
		SourceBackup: sourceBackupPath,
		ExpireTime:   timestamppb.New(expireTime),
	}

	op, err := ac.tClient.CopyBackup(ctx, req)
	if err != nil {
		return err
	}
	resp := btapb.Backup{}
	return longrunning.InternalNewOperation(ac.lroClient, op).Wait(ctx, &resp)
}

// Backups returns a BackupIterator for iterating over the backups in a cluster.
// To list backups across all of the clusters in the instance specify "-" as the cluster.
func (ac *AdminClient) Backups(ctx context.Context, cluster string) *BackupIterator {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	prefix := ac.instancePrefix()
	clusterPath := prefix + "/clusters/" + cluster

	it := &BackupIterator{}
	req := &btapb.ListBackupsRequest{
		Parent: clusterPath,
	}

	fetch := func(pageSize int, pageToken string) (string, error) {
		req.PageToken = pageToken
		if pageSize > math.MaxInt32 {
			req.PageSize = math.MaxInt32
		} else {
			req.PageSize = int32(pageSize)
		}

		var resp *btapb.ListBackupsResponse
		err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
			var err error
			resp, err = ac.tClient.ListBackups(ctx, req)
			return err
		}, retryOptions...)
		if err != nil {
			return "", err
		}
		for _, s := range resp.Backups {
			backupInfo, err := newBackupInfo(s)
			if err != nil {
				return "", fmt.Errorf("failed to parse backup proto %w", err)
			}
			it.items = append(it.items, backupInfo)
		}
		return resp.NextPageToken, nil
	}
	bufLen := func() int { return len(it.items) }
	takeBuf := func() interface{} { b := it.items; it.items = nil; return b }

	it.pageInfo, it.nextFunc = iterator.NewPageInfo(fetch, bufLen, takeBuf)

	return it
}

// newBackupInfo creates a BackupInfo struct from a btapb.Backup protocol buffer.
func newBackupInfo(backup *btapb.Backup) (*BackupInfo, error) {
	nameParts := strings.Split(backup.Name, "/")
	name := nameParts[len(nameParts)-1]
	tablePathParts := strings.Split(backup.SourceTable, "/")
	tableID := tablePathParts[len(tablePathParts)-1]

	if err := backup.StartTime.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid startTime: %v", err)
	}
	startTime := backup.GetStartTime().AsTime()

	if err := backup.EndTime.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid endTime: %v", err)
	}
	endTime := backup.GetEndTime().AsTime()

	if err := backup.ExpireTime.CheckValid(); err != nil {
		return nil, fmt.Errorf("invalid expireTime: %v", err)
	}
	expireTime := backup.GetExpireTime().AsTime()
	encryptionInfo := newEncryptionInfo(backup.EncryptionInfo)
	bi := BackupInfo{
		Name:           name,
		SourceTable:    tableID,
		SourceBackup:   backup.SourceBackup,
		SizeBytes:      backup.SizeBytes,
		StartTime:      startTime,
		EndTime:        endTime,
		ExpireTime:     expireTime,
		State:          backup.State.String(),
		EncryptionInfo: encryptionInfo,
	}

	return &bi, nil
}

// BackupIterator is an EntryIterator that iterates over log entries.
type BackupIterator struct {
	items    []*BackupInfo
	pageInfo *iterator.PageInfo
	nextFunc func() error
}

// PageInfo supports pagination. See https://godoc.org/google.golang.org/api/iterator package for details.
func (it *BackupIterator) PageInfo() *iterator.PageInfo {
	return it.pageInfo
}

// Next returns the next result. Its second return value is iterator.Done
// (https://godoc.org/google.golang.org/api/iterator) if there are no more
// results. Once Next returns Done, all subsequent calls will return Done.
func (it *BackupIterator) Next() (*BackupInfo, error) {
	if err := it.nextFunc(); err != nil {
		return nil, err
	}
	item := it.items[0]
	it.items = it.items[1:]
	return item, nil
}

// BackupInfo contains backup metadata. This struct is read-only.
type BackupInfo struct {
	Name           string
	SourceTable    string
	SourceBackup   string
	SizeBytes      int64
	StartTime      time.Time
	EndTime        time.Time
	ExpireTime     time.Time
	State          string
	EncryptionInfo *EncryptionInfo
}

// BackupInfo gets backup metadata.
func (ac *AdminClient) BackupInfo(ctx context.Context, cluster, backup string) (*BackupInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	backupPath := ac.backupPath(cluster, ac.instance, backup)

	req := &btapb.GetBackupRequest{
		Name: backupPath,
	}

	var resp *btapb.Backup
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		resp, err = ac.tClient.GetBackup(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	return newBackupInfo(resp)
}

// DeleteBackup deletes a backup in a cluster.
func (ac *AdminClient) DeleteBackup(ctx context.Context, cluster, backup string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	backupPath := ac.backupPath(cluster, ac.instance, backup)

	req := &btapb.DeleteBackupRequest{
		Name: backupPath,
	}
	_, err := ac.tClient.DeleteBackup(ctx, req)
	return err
}

// UpdateBackup updates the backup metadata in a cluster. The API only supports updating expire time.
func (ac *AdminClient) UpdateBackup(ctx context.Context, cluster, backup string, expireTime time.Time) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	backupPath := ac.backupPath(cluster, ac.instance, backup)

	expireTimestamp := timestamppb.New(expireTime)

	updateMask := &field_mask.FieldMask{}
	updateMask.Paths = append(updateMask.Paths, "expire_time")

	req := &btapb.UpdateBackupRequest{
		Backup: &btapb.Backup{
			Name:       backupPath,
			ExpireTime: expireTimestamp,
		},
		UpdateMask: updateMask,
	}
	_, err := ac.tClient.UpdateBackup(ctx, req)
	return err
}

// AuthorizedViewConf contains information about an authorized view.
type AuthorizedViewConf struct {
	TableID          string
	AuthorizedViewID string

	// Types that are valid to be assigned to AuthorizedView:
	//  *SubsetViewConf
	AuthorizedView     isAuthorizedView
	DeletionProtection DeletionProtection
}

// A private interface that currently only implemented by SubsetViewConf, ensuring that only SubsetViewConf instances are accepted as an AuthorizedView.
// In the future if a new type of AuthorizedView is introduced, it should also implements this interface.
type isAuthorizedView interface {
	isAuthorizedView()
}

func (av AuthorizedViewConf) proto() *btapb.AuthorizedView {
	var avp btapb.AuthorizedView

	switch dp := av.DeletionProtection; dp {
	case Protected:
		avp.DeletionProtection = true
	case Unprotected:
		avp.DeletionProtection = false
	default:
		break
	}

	switch avt := av.AuthorizedView.(type) {
	case *SubsetViewConf:
		avp.AuthorizedView = &btapb.AuthorizedView_SubsetView_{
			SubsetView: avt.proto(),
		}
	default:
		break
	}
	return &avp
}

// FamilySubset represents a subset of a column family.
type FamilySubset struct {
	Qualifiers        [][]byte
	QualifierPrefixes [][]byte
}

// SubsetViewConf contains configuration specific to an authorized view of subset view type.
type SubsetViewConf struct {
	RowPrefixes   [][]byte
	FamilySubsets map[string]FamilySubset
}

func (*SubsetViewConf) isAuthorizedView() {}

// AddRowPrefix adds a new row prefix to the subset view.
func (s *SubsetViewConf) AddRowPrefix(prefix []byte) {
	s.RowPrefixes = append(s.RowPrefixes, prefix)
}

func (s *SubsetViewConf) getOrCreateFamilySubset(familyName string) FamilySubset {
	if s.FamilySubsets == nil {
		s.FamilySubsets = make(map[string]FamilySubset)
	}
	if _, ok := s.FamilySubsets[familyName]; !ok {
		s.FamilySubsets[familyName] = FamilySubset{}
	}
	return s.FamilySubsets[familyName]
}

func (s SubsetViewConf) proto() *btapb.AuthorizedView_SubsetView {
	var p btapb.AuthorizedView_SubsetView
	p.RowPrefixes = append(p.RowPrefixes, s.RowPrefixes...)
	if p.FamilySubsets == nil {
		p.FamilySubsets = make(map[string]*btapb.AuthorizedView_FamilySubsets)
	}
	for familyName, subset := range s.FamilySubsets {
		p.FamilySubsets[familyName] = &btapb.AuthorizedView_FamilySubsets{
			Qualifiers:        subset.Qualifiers,
			QualifierPrefixes: subset.QualifierPrefixes,
		}
	}
	return &p
}

// AddFamilySubsetQualifier adds an individual column qualifier to be included in a subset view.
func (s *SubsetViewConf) AddFamilySubsetQualifier(familyName string, qualifier []byte) {
	fs := s.getOrCreateFamilySubset(familyName)
	fs.Qualifiers = append(fs.Qualifiers, qualifier)
	s.FamilySubsets[familyName] = fs
}

// AddFamilySubsetQualifierPrefix adds a prefix for column qualifiers to be included in a subset view.
func (s *SubsetViewConf) AddFamilySubsetQualifierPrefix(familyName string, qualifierPrefix []byte) {
	fs := s.getOrCreateFamilySubset(familyName)
	fs.QualifierPrefixes = append(fs.QualifierPrefixes, qualifierPrefix)
	s.FamilySubsets[familyName] = fs
}

// CreateAuthorizedView creates a new authorized view in a table.
func (ac *AdminClient) CreateAuthorizedView(ctx context.Context, conf *AuthorizedViewConf) error {
	if conf.TableID == "" || conf.AuthorizedViewID == "" {
		return errors.New("both AuthorizedViewID and TableID are required")
	}
	if _, ok := conf.AuthorizedView.(*SubsetViewConf); !ok {
		return errors.New("SubsetView must be specified in AuthorizedViewConf")
	}

	ctx = mergeOutgoingMetadata(ctx, ac.md)
	req := &btapb.CreateAuthorizedViewRequest{
		Parent:           fmt.Sprintf("%s/tables/%s", ac.instancePrefix(), conf.TableID),
		AuthorizedViewId: conf.AuthorizedViewID,
		AuthorizedView:   conf.proto(),
	}
	_, err := ac.tClient.CreateAuthorizedView(ctx, req)
	return err
}

// AuthorizedViewInfo contains authorized view metadata. This struct is read-only.
type AuthorizedViewInfo struct {
	TableID          string
	AuthorizedViewID string

	AuthorizedView     isAuthorizedViewInfo
	DeletionProtection DeletionProtection
}

type isAuthorizedViewInfo interface {
	isAuthorizedViewInfo()
}

// SubsetViewInfo contains read-only SubsetView metadata.
type SubsetViewInfo struct {
	RowPrefixes   [][]byte
	FamilySubsets map[string]FamilySubset
}

func (*SubsetViewInfo) isAuthorizedViewInfo() {}

func (s *SubsetViewInfo) fillInfo(internal *btapb.AuthorizedView_SubsetView) {
	s.RowPrefixes = [][]byte{}
	s.RowPrefixes = append(s.RowPrefixes, internal.RowPrefixes...)
	if s.FamilySubsets == nil {
		s.FamilySubsets = make(map[string]FamilySubset)
	}
	for k, v := range internal.FamilySubsets {
		s.FamilySubsets[k] = FamilySubset{
			Qualifiers:        v.Qualifiers,
			QualifierPrefixes: v.QualifierPrefixes,
		}
	}
}

// AuthorizedViewInfo retrieves information about an authorized view.
func (ac *AdminClient) AuthorizedViewInfo(ctx context.Context, tableID, authorizedViewID string) (*AuthorizedViewInfo, error) {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	req := &btapb.GetAuthorizedViewRequest{
		Name: fmt.Sprintf("%s/tables/%s/authorizedViews/%s", ac.instancePrefix(), tableID, authorizedViewID),
	}
	var res *btapb.AuthorizedView

	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = ac.tClient.GetAuthorizedView(ctx, req)
		return err
	}, retryOptions...)

	if err != nil {
		return nil, err
	}

	av := &AuthorizedViewInfo{TableID: tableID, AuthorizedViewID: authorizedViewID}
	if res.DeletionProtection {
		av.DeletionProtection = Protected
	} else {
		av.DeletionProtection = Unprotected
	}
	if res.GetSubsetView() != nil {
		s := SubsetViewInfo{}
		s.fillInfo(res.GetSubsetView())
		av.AuthorizedView = &s
	}
	return av, nil
}

// AuthorizedViews returns a list of the authorized views in the table.
func (ac *AdminClient) AuthorizedViews(ctx context.Context, tableID string) ([]string, error) {
	names := []string{}
	prefix := fmt.Sprintf("%s/tables/%s", ac.instancePrefix(), tableID)

	req := &btapb.ListAuthorizedViewsRequest{
		Parent: prefix,
		View:   btapb.AuthorizedView_NAME_ONLY,
	}
	var res *btapb.ListAuthorizedViewsResponse
	err := gax.Invoke(ctx, func(ctx context.Context, _ gax.CallSettings) error {
		var err error
		res, err = ac.tClient.ListAuthorizedViews(ctx, req)
		return err
	}, retryOptions...)
	if err != nil {
		return nil, err
	}

	for _, av := range res.AuthorizedViews {
		names = append(names, strings.TrimPrefix(av.Name, prefix+"/authorizedViews/"))
	}
	return names, nil
}

// UpdateAuthorizedViewConf contains all the information necessary to update or partial update an authorized view.
type UpdateAuthorizedViewConf struct {
	AuthorizedViewConf AuthorizedViewConf
	IgnoreWarnings     bool
}

// UpdateAuthorizedView updates an authorized view in a table according to the given configuration.
func (ac *AdminClient) UpdateAuthorizedView(ctx context.Context, conf UpdateAuthorizedViewConf) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	if conf.AuthorizedViewConf.TableID == "" || conf.AuthorizedViewConf.AuthorizedViewID == "" {
		return errors.New("both AuthorizedViewID and TableID is required")
	}
	av := conf.AuthorizedViewConf.proto()
	av.Name = ac.authorizedViewPath(conf.AuthorizedViewConf.TableID, conf.AuthorizedViewConf.AuthorizedViewID)

	updateMask := &field_mask.FieldMask{
		Paths: []string{},
	}
	if conf.AuthorizedViewConf.DeletionProtection != None {
		updateMask.Paths = append(updateMask.Paths, "deletion_protection")
	}
	if _, ok := conf.AuthorizedViewConf.AuthorizedView.(*SubsetViewConf); ok {
		updateMask.Paths = append(updateMask.Paths, "subset_view")
	}
	req := &btapb.UpdateAuthorizedViewRequest{
		AuthorizedView: av,
		UpdateMask:     updateMask,
		IgnoreWarnings: conf.IgnoreWarnings,
	}
	lro, err := ac.tClient.UpdateAuthorizedView(ctx, req)
	if err != nil {
		return fmt.Errorf("error from update authorized view: %w", err)
	}
	var res btapb.AuthorizedView
	op := longrunning.InternalNewOperation(ac.lroClient, lro)
	if err = op.Wait(ctx, &res); err != nil {
		return fmt.Errorf("error from operation: %v", err)
	}
	return nil
}

// DeleteAuthorizedView deletes an authorized view in a table.
func (ac *AdminClient) DeleteAuthorizedView(ctx context.Context, tableID, authorizedViewID string) error {
	ctx = mergeOutgoingMetadata(ctx, ac.md)
	req := &btapb.DeleteAuthorizedViewRequest{
		Name: ac.authorizedViewPath(tableID, authorizedViewID),
	}
	_, err := ac.tClient.DeleteAuthorizedView(ctx, req)
	return err
}
