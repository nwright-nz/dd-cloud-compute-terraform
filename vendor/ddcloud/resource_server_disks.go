package ddcloud

import (
	"fmt"
	"log"

	"github.com/DimensionDataResearch/dd-cloud-compute-terraform/models"
	"github.com/DimensionDataResearch/dd-cloud-compute-terraform/retry"
	"github.com/DimensionDataResearch/go-dd-cloud-compute/compute"
	"github.com/hashicorp/terraform/helper/schema"
)

const (
	resourceKeyServerDisk       = "disk"
	resourceKeyServerDiskID     = "disk_id"
	resourceKeyServerDiskUnitID = "scsi_unit_id"
	resourceKeyServerDiskSizeGB = "size_gb"
	resourceKeyServerDiskSpeed  = "speed"
	// TODO: Consider adding "disk_type" property ("image" or "additional")
)

func schemaServerDisk() *schema.Schema {
	return &schema.Schema{
		Type:        schema.TypeSet,
		Optional:    true,
		Computed:    true,
		Default:     nil,
		Description: "The set of virtual disks attached to the server",
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				resourceKeyServerDiskID: &schema.Schema{
					Type:        schema.TypeString,
					Computed:    true,
					Description: "The CloudControl identifier for the virtual disk (computed when the disk is first created)",
				},
				resourceKeyServerDiskUnitID: &schema.Schema{
					Type:        schema.TypeInt,
					Required:    true,
					Description: "The SCSI Logical Unit Number (LUN) for the disk",
				},
				resourceKeyServerDiskSizeGB: &schema.Schema{
					Type:        schema.TypeInt,
					Required:    true,
					Description: "The size (in GB) of the disk",
				},
				resourceKeyServerDiskSpeed: &schema.Schema{
					Type:        schema.TypeString,
					Optional:    true,
					Default:     "STANDARD",
					StateFunc:   normalizeSpeed,
					Description: "The disk speed",
				},
			},
		},
		Set: hashDisk,
	}
}

// When creating a server resource, synchronise the server's disks with its resource data.
// imageDisks refers to the newly-deployed server's collection of disks (i.e. image disks).
func createDisks(imageDisks []compute.VirtualMachineDisk, data *schema.ResourceData, providerState *providerState) error {
	propertyHelper := propertyHelper(data)
	serverID := data.Id()

	log.Printf("Configuring image disks for server '%s'...", serverID)

	configuredDisks := propertyHelper.GetServerDisks()
	log.Printf("Configuration for server '%s' specifies %d disks: %#v.", serverID, len(configuredDisks), configuredDisks)

	if len(configuredDisks) == 0 {
		// Since this is the first time, populate image disks.
		serverDisks := models.NewServerDisksFromVirtualMachineDisks(imageDisks)
		propertyHelper.SetServerDisks(serverDisks)
		propertyHelper.SetPartial(resourceKeyServerDisk)

		log.Printf("Server '%s' now has %d disks: %#v.", serverID, len(serverDisks), serverDisks)

		return nil
	}

	apiClient := providerState.Client()

	var server *compute.Server
	server, err := apiClient.GetServer(serverID)
	if err != nil {
		return err
	}
	if server == nil {
		return fmt.Errorf("Cannot find server with Id '%s'", serverID)
	}

	// After initial server deployment, we only need to handle disks that were part of the original server image (and of those, only ones we need to modify after the initial deployment completed deployment).
	log.Printf("Configure image disks for server '%s'...", serverID)
	actualDisks := models.NewServerDisksFromVirtualMachineDisks(server.Disks)
	addDisks, modifyDisks, _ := configuredDisks.SplitByAction(actualDisks) // Ignore removeDisks since not all disks have been created yet
	if addDisks.IsEmpty() && modifyDisks.IsEmpty() {
		log.Printf("No post-deploy changes required for disks of server '%s'.", serverID)

		return nil
	}
	err = processModifyDisks(modifyDisks, data, providerState)
	if err != nil {
		return err
	}

	log.Printf("Configure additional disks for server '%s'...", serverID)
	err = processAddDisks(addDisks, data, providerState)
	if err != nil {
		return err
	}

	return nil
}

// When updating a server resource, synchronise the server's image disk attributes with its resource data
// Removes image disks from existingDisksByUnitID as they are processed, leaving only additional disks.
func updateDisks(data *schema.ResourceData, providerState *providerState) error {
	propertyHelper := propertyHelper(data)
	serverID := data.Id()

	log.Printf("Configure image disks for server '%s'...", serverID)

	apiClient := providerState.Client()
	server, err := apiClient.GetServer(serverID)
	if err != nil {
		return err
	}
	if server == nil {
		data.SetId("")

		return fmt.Errorf("Server '%s' has been deleted.", serverID)
	}
	actualDisks := models.NewServerDisksFromVirtualMachineDisks(server.Disks)

	configuredDisks := propertyHelper.GetServerDisks()
	log.Printf("Configuration for server '%s' specifies %d disks: %#v.", serverID, len(configuredDisks), configuredDisks)

	if configuredDisks.IsEmpty() {
		// No explicitly-configured disks.
		propertyHelper.SetServerDisks(
			models.NewServerDisksFromVirtualMachineDisks(server.Disks),
		)
		propertyHelper.SetPartial(resourceKeyServerDisk)

		log.Printf("Server '%s' now has %d disks: %#v.", serverID, len(server.Disks), server.Disks)

		return nil
	}

	addDisks, modifyDisks, removeDisks := configuredDisks.SplitByAction(actualDisks)
	if addDisks.IsEmpty() && modifyDisks.IsEmpty() && removeDisks.IsEmpty() {
		log.Printf("No post-deploy changes required for disks of server '%s'.", serverID)

		return nil
	}

	// First remove any disks that are no longer required.
	err = processRemoveDisks(removeDisks, data, providerState)
	if err != nil {
		return err
	}

	// Then modify existing disks
	err = processModifyDisks(modifyDisks, data, providerState)
	if err != nil {
		return err
	}

	// Finally, add new disks
	err = processAddDisks(addDisks, data, providerState)
	if err != nil {
		return err
	}

	return nil
}

// Process the collection of disks that need to be added to the server.
func processAddDisks(addDisks models.ServerDisks, data *schema.ResourceData, providerState *providerState) error {
	propertyHelper := propertyHelper(data)
	serverID := data.Id()

	providerSettings := providerState.Settings()
	apiClient := providerState.Client()

	for _, addDisk := range addDisks {
		var addDiskID string

		operationDescription := fmt.Sprintf("Add disk with SCSI unit ID %d to server '%s'",
			addDisk.SCSIUnitID,
			serverID,
		)
		err := providerState.Retry().Action(operationDescription, providerSettings.RetryTimeout, func(context retry.Context) {
			asyncLock := providerState.AcquireAsyncOperationLock(operationDescription)
			defer asyncLock.Release()

			var addDiskError error
			addDiskID, addDiskError = apiClient.AddDiskToServer(
				serverID,
				addDisk.SCSIUnitID,
				addDisk.SizeGB,
				addDisk.Speed,
			)
			if compute.IsResourceBusyError(addDiskError) {
				context.Retry()
			} else if addDiskError != nil {
				context.Fail(addDiskError)
			}
		})
		if err == nil {
			return err
		}
		addDisk.ID = &addDiskID

		log.Printf("Adding disk '%s' (%dGB, speed = '%s') with SCSI unit ID %d to server '%s'...",
			addDiskID,
			addDisk.SizeGB,
			addDisk.Speed,
			addDisk.SCSIUnitID,
			serverID,
		)

		var resource compute.Resource
		resource, err = apiClient.WaitForChange(
			compute.ResourceTypeServer,
			serverID,
			"Add disk",
			resourceUpdateTimeoutServer,
		)
		if err != nil {
			return err
		}

		server := resource.(*compute.Server)
		propertyHelper.SetServerDisks(
			models.NewServerDisksFromVirtualMachineDisks(server.Disks),
		)
		propertyHelper.SetPartial(resourceKeyServerDisk)

		log.Printf("Server '%s' now has %d disks: %#v.", serverID, len(server.Disks), server.Disks)

		log.Printf("Added disk '%s' with SCSI unit ID %d to server '%s'.",
			addDiskID,
			addDisk.SCSIUnitID,
			serverID,
		)
	}

	return nil
}

// Process the collection of disks whose configuration needs to be modified.
//
// Disk Ids must already be populated.
func processModifyDisks(modifyDisks models.ServerDisks, data *schema.ResourceData, providerState *providerState) error {
	propertyHelper := propertyHelper(data)
	serverID := data.Id()

	providerSettings := providerState.Settings()
	apiClient := providerState.Client()

	server, err := apiClient.GetServer(serverID)
	if err != nil {
		return err
	}
	if server == nil {
		data.SetId("")

		return fmt.Errorf("Server '%s' has been deleted.", serverID)
	}
	actualDisks := models.NewServerDisksFromVirtualMachineDisks(server.Disks)
	actualDisksByUnitID := actualDisks.ByUnitID()

	for _, modifyDisk := range modifyDisks {
		actualImageDisk := actualDisksByUnitID[modifyDisk.SCSIUnitID]
		imageDiskID := *actualImageDisk.ID

		// Can't shrink disk, only grow it.
		if modifyDisk.SizeGB < actualImageDisk.SizeGB {
			return fmt.Errorf(
				"Cannot resize disk '%s' in server '%s' from %d to GB to %d (for now, disks can only be expanded).",
				imageDiskID,
				serverID,
				actualImageDisk.SizeGB,
				modifyDisk.SizeGB,
			)
		}

		// Do we need to expand the disk?
		if modifyDisk.SizeGB > actualImageDisk.SizeGB {
			log.Printf(
				"Expanding disk '%s' in server '%s' (from %d GB to %d GB)...",
				imageDiskID,
				serverID,
				actualImageDisk.SizeGB,
				modifyDisk.SizeGB,
			)

			operationDescription := fmt.Sprintf("Expand disk '%s' in server '%s'", imageDiskID, serverID)
			err = providerState.Retry().Action(operationDescription, providerSettings.RetryTimeout, func(context retry.Context) {
				asyncLock := providerState.AcquireAsyncOperationLock(operationDescription)
				defer asyncLock.Release()

				response, resizeError := apiClient.ResizeServerDisk(serverID, imageDiskID, modifyDisk.SizeGB)
				if compute.IsResourceBusyError(resizeError) {
					context.Retry()
				} else if resizeError != nil {
					context.Fail(resizeError)
				}
				if response.Result != compute.ResultSuccess {
					context.Fail(response.ToError(
						"Unexpected result '%s' when resizing server disk '%s' for server '%s'.",
						response.Result,
						imageDiskID,
						serverID,
					))
				}
			})
			if err != nil {
				return err
			}

			resource, err := apiClient.WaitForChange(
				compute.ResourceTypeServer,
				serverID,
				"Resize disk",
				resourceUpdateTimeoutServer,
			)
			if err != nil {
				return err
			}

			server := resource.(*compute.Server)
			propertyHelper.SetServerDisks(
				models.NewServerDisksFromVirtualMachineDisks(server.Disks),
			)
			propertyHelper.SetPartial(resourceKeyServerDisk)

			log.Printf(
				"Resized disk '%s' for server '%s' (from %d to GB to %d).",
				imageDiskID,
				serverID,
				actualImageDisk.SizeGB,
				modifyDisk.SizeGB,
			)
		}

		// Do we need to change the disk speed?
		if modifyDisk.Speed != actualImageDisk.Speed {
			log.Printf(
				"Changing speed of disk '%s' in server '%s' (from '%s' to '%s')...",
				imageDiskID,
				serverID,
				actualImageDisk.Speed,
				modifyDisk.Speed,
			)

			operationDescription := fmt.Sprintf("Change speed of disk '%s' in server '%s'", imageDiskID, serverID)
			err = providerState.Retry().Action(operationDescription, providerSettings.RetryTimeout, func(context retry.Context) {
				asyncLock := providerState.AcquireAsyncOperationLock(operationDescription)
				defer asyncLock.Release()

				response, resizeError := apiClient.ChangeServerDiskSpeed(serverID, imageDiskID, modifyDisk.Speed)
				if compute.IsResourceBusyError(resizeError) {
					context.Retry()
				} else if resizeError != nil {
					context.Fail(resizeError)
				}
				if response.Result != compute.ResultSuccess {
					context.Fail(response.ToError(
						"Unexpected result '%s' when resizing server disk '%s' for server '%s'.",
						response.Result,
						imageDiskID,
						serverID,
					))
				}
			})
			if err != nil {
				return err
			}

			resource, err := apiClient.WaitForChange(
				compute.ResourceTypeServer,
				serverID,
				"Resize disk",
				resourceUpdateTimeoutServer,
			)
			if err != nil {
				return err
			}

			server = resource.(*compute.Server)
			propertyHelper.SetServerDisks(
				models.NewServerDisksFromVirtualMachineDisks(server.Disks),
			)
			propertyHelper.SetPartial(resourceKeyServerDisk)

			log.Printf(
				"Resized disk '%s' for server '%s' (from %d to GB to %d).",
				imageDiskID,
				serverID,
				actualImageDisk.SizeGB,
				modifyDisk.SizeGB,
			)
		}
	}

	return nil
}

// Process the collection of disks that need to be removed.
//
// Disk Ids must already be populated.
func processRemoveDisks(removeDisks models.ServerDisks, data *schema.ResourceData, providerState *providerState) error {
	propertyHelper := propertyHelper(data)
	serverID := data.Id()

	providerSettings := providerState.Settings()
	apiClient := providerState.Client()

	server, err := apiClient.GetServer(serverID)
	if err != nil {
		return err
	}
	if server == nil {
		data.SetId("")

		return fmt.Errorf("Server '%s' has been deleted.", serverID)
	}

	for _, removeDisk := range removeDisks {
		removeDiskID := *removeDisk.ID

		log.Printf("Remove disk '%s' (SCSI unit Id %d) from server '%s'...",
			removeDiskID,
			removeDisk.SCSIUnitID,
			serverID,
		)

		operationDescription := fmt.Sprintf("Remove disk '%s' from server '%s'", removeDiskID, serverID)
		err = providerState.Retry().Action(operationDescription, providerSettings.RetryTimeout, func(context retry.Context) {
			asyncLock := providerState.AcquireAsyncOperationLock(operationDescription)
			defer asyncLock.Release()

			removeError := apiClient.RemoveDiskFromServer(removeDiskID)
			if compute.IsResourceBusyError(removeError) {
				context.Retry()
			} else if removeError != nil {
				context.Fail(removeError)
			}
		})
		if err != nil {
			return err
		}

		resource, err := apiClient.WaitForChange(
			compute.ResourceTypeServer,
			serverID,
			"Remove disk",
			resourceUpdateTimeoutServer,
		)
		if err != nil {
			return err
		}

		server := resource.(*compute.Server)
		propertyHelper.SetServerDisks(
			models.NewServerDisksFromVirtualMachineDisks(server.Disks),
		)
		propertyHelper.SetPartial(resourceKeyServerDisk)

		log.Printf(
			"Removed disk '%s' from server '%s'.",
			removeDiskID,
			serverID,
		)
	}

	return nil
}

func hashDiskUnitID(item interface{}) int {
	disk, ok := item.(compute.VirtualMachineDisk)
	if ok {
		return disk.SCSIUnitID
	}

	diskData := item.(map[string]interface{})

	return diskData[resourceKeyServerDiskUnitID].(int)
}

func hashDisk(item interface{}) int {
	disk, ok := item.(compute.VirtualMachineDisk)
	if ok {
		return schema.HashString(fmt.Sprintf(
			"%d/%d/%s",
			disk.SCSIUnitID,
			disk.SizeGB,
			disk.Speed,
		))
	}

	diskData := item.(map[string]interface{})

	return schema.HashString(fmt.Sprintf(
		"%d/%d/%s",
		diskData[resourceKeyServerDiskUnitID].(int),
		diskData[resourceKeyServerDiskSizeGB].(int),
		diskData[resourceKeyServerDiskSpeed].(string),
	))
}
