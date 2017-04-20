package ddcloud

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DimensionDataResearch/dd-cloud-compute-terraform/models"
	"github.com/DimensionDataResearch/go-dd-cloud-compute/compute"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"
)

/*
 * Acceptance-test configurations.
 */

// A server with a single (default) storage controller (and its image disk).
//
// Essentially, this should be a no-op.
func testAccDDCloudStorageController1DefaultWithImageDisk() string {
	return `
		provider "ddcloud" {
			region		= "AU"
		}

		resource "ddcloud_networkdomain" "acc_test_domain" {
			name		= "acc-test-networkdomain"
			description	= "Network domain for Terraform acceptance test."
			datacenter	= "AU9"
		}

		resource "ddcloud_vlan" "acc_test_vlan" {
			name				= "acc-test-vlan"
			description 		= "VLAN for Terraform acceptance test."

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"

			ipv4_base_address	= "192.168.17.0"
			ipv4_prefix_size	= 24
		}

		resource "ddcloud_server" "acc_test_server" {
			name				= "AccTestStorageControllerServer"
			description 		= "Server for storage controller acceptance test"
			admin_password		= "snausages!"

			memory_gb			= 8

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"
			
			primary_network_adapter {
				vlan            = "${ddcloud_vlan.acc_test_vlan.id}"
				ipv4            = "192.168.17.20"
			}

			dns_primary			= "8.8.8.8"
			dns_secondary		= "8.8.4.4"

			image				= "CentOS 7 64-bit 2 CPU"

			auto_start			= false
		}

		resource "ddcloud_storage_controller" "acc_test_server_controller_0" {
			server				= "${ddcloud_server.acc_test_server.id}"
			scsi_bus_number		= 0

			# Image disk
			disk {
				scsi_unit_id    = 0
				size_gb         = 10
				speed           = "STANDARD"
			}
		}
	`
}

// The default storage controller for a server (and its image disk and one additional disk).
func testAccDDCloudStorageController1DefaultWithAdditional1Disk() string {
	return `
		provider "ddcloud" {
			region		= "AU"
		}

		resource "ddcloud_networkdomain" "acc_test_domain" {
			name		= "acc-test-networkdomain"
			description	= "Network domain for Terraform acceptance test."
			datacenter	= "AU9"
		}

		resource "ddcloud_vlan" "acc_test_vlan" {
			name				= "acc-test-vlan"
			description 		= "VLAN for Terraform acceptance test."

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"

			ipv4_base_address	= "192.168.17.0"
			ipv4_prefix_size	= 24
		}

		resource "ddcloud_server" "acc_test_server" {
			name				= "AccTestStorageControllerServer"
			description 		= "Server for storage controller acceptance test"
			admin_password		= "snausages!"

			memory_gb			= 8

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"
			
			primary_network_adapter {
				vlan            = "${ddcloud_vlan.acc_test_vlan.id}"
				ipv4            = "192.168.17.20"
			}

			dns_primary			= "8.8.8.8"
			dns_secondary		= "8.8.4.4"

			image				= "CentOS 7 64-bit 2 CPU"

			auto_start			= false
		}

		resource "ddcloud_storage_controller" "acc_test_server_controller_0" {
			server				= "${ddcloud_server.acc_test_server.id}"
			scsi_bus_number		= 0

			# Image disk
			disk {
				scsi_unit_id    = 0
				size_gb         = 10
				speed           = "STANDARD"
			}

			# Additional disk
			disk {
				scsi_unit_id    = 1
				size_gb         = 20
				speed           = "STANDARD"
			}
		}
	`
}

// A server with 2 storage controllers, each with a single disk.
//
// Pass false for withAdditionalController to leave out the second controller.
func testAccDDCloudStorageController2With1DiskEach(withSecondController bool) string {
	secondControllerConfiguration := ""

	if withSecondController {
		secondControllerConfiguration = `
			resource "ddcloud_storage_controller" "acc_test_server_controller_1" {
				server				= "${ddcloud_server.acc_test_server.id}"
				scsi_bus_number		= 1
				adapter_type		= "LSI_LOGIC_SAS"

				disk {
					scsi_unit_id    = 0
					size_gb         = 20
					speed           = "STANDARD"
				}
			}
		`
	}

	return fmt.Sprintf(`
		provider "ddcloud" {
			region		= "AU"
		}

		resource "ddcloud_networkdomain" "acc_test_domain" {
			name		= "acc-test-networkdomain"
			description	= "Network domain for Terraform acceptance test."
			datacenter	= "AU9"
		}

		resource "ddcloud_vlan" "acc_test_vlan" {
			name				= "acc-test-vlan"
			description 		= "VLAN for Terraform acceptance test."

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"

			ipv4_base_address	= "192.168.17.0"
			ipv4_prefix_size	= 24
		}

		resource "ddcloud_server" "acc_test_server" {
			name				= "AccTestStorageControllerServer"
			description 		= "Server for storage controller acceptance test"
			admin_password		= "snausages!"

			memory_gb			= 8

			networkdomain 		= "${ddcloud_networkdomain.acc_test_domain.id}"
			
			primary_network_adapter {
				vlan            = "${ddcloud_vlan.acc_test_vlan.id}"
				ipv4            = "192.168.17.20"
			}

			dns_primary			= "8.8.8.8"
			dns_secondary		= "8.8.4.4"

			image				= "CentOS 7 64-bit 2 CPU"

			auto_start			= false
		}

		resource "ddcloud_storage_controller" "acc_test_server_controller_0" {
			server				= "${ddcloud_server.acc_test_server.id}"
			scsi_bus_number		= 0

			disk {
				scsi_unit_id    = 0
				size_gb         = 10
				speed           = "STANDARD"
			}
		}

		%s
	`, secondControllerConfiguration)
}

/*
 * Acceptance tests.
 */

// Acceptance test for ddcloud_storage_controller (default with image disk):
//
// Create the storage controller and verify that it is attached to the server with the correct configuration.
func TestAccStorageController1DefaultWithImageDiskCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testCheckDDCloudStorageControllerDestroy,
			testCheckDDCloudServerDestroy,
			testCheckDDCloudVLANDestroy,
			testCheckDDCloudNetworkDomainDestroy,
		),
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccDDCloudStorageController1DefaultWithImageDisk(),
				Check: resource.ComposeTestCheckFunc(
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_0", true),
					testCheckDDCloudStorageControllerMatches("ddcloud_storage_controller.acc_test_server_controller_0", compute.VirtualMachineSCSIController{
						BusNumber:   0,
						AdapterType: compute.StorageControllerAdapterTypeLSILogicParallel,
					}),
					testCheckDDCloudStorageControllerDiskMatches("ddcloud_storage_controller.acc_test_server_controller_0",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
					),
					testCheckDDCloudServerDiskMatches("ddcloud_server.acc_test_server",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
					),
				),
			},
		},
	})
}

// Acceptance test for ddcloud_storage_controller (default with image disk and 1 additional disk):
//
// Create the storage controller and verify that it is attached to the server with the correct configuration.
func TestAccStorageController1DefaultWithAdditional1DiskCreate(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testCheckDDCloudStorageControllerDestroy,
			testCheckDDCloudServerDestroy,
			testCheckDDCloudVLANDestroy,
			testCheckDDCloudNetworkDomainDestroy,
		),
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccDDCloudStorageController1DefaultWithImageDisk(),
				Check: resource.ComposeTestCheckFunc(
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_0", true),
					testCheckDDCloudStorageControllerMatches("ddcloud_storage_controller.acc_test_server_controller_0", compute.VirtualMachineSCSIController{
						BusNumber:   0,
						AdapterType: compute.StorageControllerAdapterTypeLSILogicParallel,
					}),
					testCheckDDCloudServerDiskMatches("ddcloud_server.acc_test_server",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
						testDisk(1, 20, compute.ServerDiskSpeedStandard),
					),
					testCheckDDCloudStorageControllerDiskMatches("ddcloud_storage_controller.acc_test_server_controller_0",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
						testDisk(1, 20, compute.ServerDiskSpeedStandard),
					),
				),
			},
		},
	})
}

// Acceptance test for ddcloud_storage_controller (server with 2 storage controllers, each with a single disk):
//
// Create the storage controllers, the remove one from configuration and verify that it is removed from the server.
func TestAccStorageController2With1DiskEachRemoveSecondController(t *testing.T) {
	resource.Test(t, resource.TestCase{
		Providers: testAccProviders,
		CheckDestroy: resource.ComposeTestCheckFunc(
			testCheckDDCloudStorageControllerDestroy,
			testCheckDDCloudServerDestroy,
			testCheckDDCloudVLANDestroy,
			testCheckDDCloudNetworkDomainDestroy,
		),
		Steps: []resource.TestStep{
			resource.TestStep{
				Config: testAccDDCloudStorageController2With1DiskEach(true /* with second storage controller */),
				Check: resource.ComposeTestCheckFunc(
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_0", true),
					testCheckDDCloudStorageControllerMatches("ddcloud_storage_controller.acc_test_server_controller_0", compute.VirtualMachineSCSIController{
						BusNumber:   0,
						AdapterType: compute.StorageControllerAdapterTypeLSILogicParallel,
					}),
					testCheckDDCloudStorageControllerDiskMatches("ddcloud_storage_controller.acc_test_server_controller_0",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
					),
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_1", true),
					testCheckDDCloudStorageControllerMatches("ddcloud_storage_controller.acc_test_server_controller_1", compute.VirtualMachineSCSIController{
						BusNumber:   1,
						AdapterType: compute.StorageControllerAdapterTypeLSILogicSAS,
					}),
					testCheckDDCloudStorageControllerDiskMatches("ddcloud_storage_controller.acc_test_server_controller_1",
						testDisk(0, 20, compute.ServerDiskSpeedStandard),
					),
					testCheckDDCloudServerDiskMatches("ddcloud_server.acc_test_server",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
						testDisk(0, 20, compute.ServerDiskSpeedStandard),
					),
				),
			},
			resource.TestStep{
				Config: testAccDDCloudStorageController2With1DiskEach(false /* without second storage controller */),
				Check: resource.ComposeTestCheckFunc(
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_0", true),
					testCheckDDCloudStorageControllerMatches("ddcloud_storage_controller.acc_test_server_controller_0", compute.VirtualMachineSCSIController{
						BusNumber:   0,
						AdapterType: compute.StorageControllerAdapterTypeLSILogicParallel,
					}),
					testCheckDDCloudStorageControllerDiskMatches("ddcloud_storage_controller.acc_test_server_controller_0",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
					),
					testCheckDDCloudStorageControllerExists("ddcloud_storage_controller.acc_test_server_controller_1", false),
					testCheckDDCloudServerDiskMatches("ddcloud_server.acc_test_server",
						testDisk(0, 10, compute.ServerDiskSpeedStandard),
					),
				),
			},
		},
	})
}

/*
 * Acceptance-test checks.
 */

// Acceptance test check for ddcloud_storage_controller:
//
// Check if the storage controller exists.
func testCheckDDCloudStorageControllerExists(name string, exists bool) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		res, ok := state.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("resource '%s' not found", name)
		}

		controllerID := res.Primary.ID
		serverID := res.Primary.Attributes[resourceKeyStorageControllerServerID]

		client := testAccProvider.Meta().(*providerState).Client()
		server, err := client.GetServer(serverID)
		if err != nil {
			return fmt.Errorf("bad: get server '%s': %s", serverID, err)
		}
		if exists && server == nil {
			return fmt.Errorf("bad: server not found with Id '%s'", serverID)
		}

		storageController := server.SCSIControllers.GetByID(controllerID)
		if exists && storageController == nil {
			return fmt.Errorf("bad: storage controller '%s' not found in server '%s'", controllerID, serverID)
		} else if !exists && storageController != nil {
			return fmt.Errorf("bad: storage controller '%s' still exists in server '%s'", controllerID, serverID)
		}

		return nil
	}
}

// Acceptance test check for ddcloud_storage_controller:
//
// Check if the storage controller's configuration matches the expected configuration.
func testCheckDDCloudStorageControllerMatches(resourceName string, expected compute.VirtualMachineSCSIController) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		storageControllerResource, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("not found: %s", resourceName)
		}

		controllerID := storageControllerResource.Primary.ID
		serverID := storageControllerResource.Primary.Attributes[resourceKeyStorageControllerServerID]

		client := testAccProvider.Meta().(*providerState).Client()
		server, err := client.GetServer(serverID)
		if err != nil {
			return fmt.Errorf("bad: get server '%s': %s", serverID, err)
		}
		if server == nil {
			return fmt.Errorf("bad: server '%s' not found", serverID)
		}

		actual := server.SCSIControllers.GetByID(controllerID)
		if actual == nil {
			return fmt.Errorf("bad: storage controller '%s' not found in server '%s'", controllerID, serverID)
		}

		if actual.BusNumber != expected.BusNumber {
			return fmt.Errorf("bad: storage controller '%s' has bus %d (expected %d)", controllerID, actual.BusNumber, expected.BusNumber)
		}

		if actual.AdapterType != expected.AdapterType {
			return fmt.Errorf("bad: storage controller '%s' has adapter type '%s' (expected '%s')", controllerID, actual.AdapterType, expected.AdapterType)
		}

		return nil
	}
}

// Acceptance test check for ddcloud_storage_controller:
//
// Check if the storage controller's disk configuration matches the expected configuration.
func testCheckDDCloudStorageControllerDiskMatches(resourceName string, expected ...models.Disk) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		storageControllerResource, ok := state.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource '%s' not found", resourceName)
		}

		controllerID := storageControllerResource.Primary.ID
		serverID := storageControllerResource.Primary.Attributes[resourceKeyStorageControllerServerID]

		client := testAccProvider.Meta().(*providerState).Client()
		server, err := client.GetServer(serverID)
		if err != nil {
			return fmt.Errorf("bad: get server '%s': %s", serverID, err)
		}
		if server == nil {
			return fmt.Errorf("bad: server '%s' not found", serverID)
		}

		actualSCSIController := server.SCSIControllers.GetByID(controllerID)
		if actualSCSIController == nil {
			return fmt.Errorf("bad: storage controller '%s' not found in server '%s'", controllerID, serverID)
		}

		var validationMessages []string
		expectedDisksBySCSIPath := models.Disks(expected).BySCSIPath()
		for _, actualDisk := range actualSCSIController.Disks {
			scsiPath := models.SCSIPath(actualSCSIController.BusNumber, actualDisk.SCSIUnitID)
			expectedDisk, ok := expectedDisksBySCSIPath[scsiPath]
			if !ok {
				validationMessages = append(validationMessages, fmt.Sprintf(
					"found unexpected disk '%s' on SCSI controller '%s' (bus %d) with SCSI unit ID %d.",
					actualDisk.ID,
					actualSCSIController.ID,
					actualSCSIController.BusNumber,
					actualDisk.SCSIUnitID,
				))

				continue
			}
			delete(expectedDisksBySCSIPath, scsiPath) // Eliminate it from the list of unmatched disks.

			if actualDisk.SizeGB != expectedDisk.SizeGB {
				validationMessages = append(validationMessages, fmt.Sprintf(
					"disk '%s' on SCSI controller '%s' (bus %d) with SCSI unit ID %d has size %dGB (expected %dGB).",
					actualDisk.ID,
					actualSCSIController.ID,
					actualSCSIController.BusNumber,
					actualDisk.SCSIUnitID,
					actualDisk.SizeGB,
					expectedDisk.SizeGB,
				))
			}

			if actualDisk.Speed != expectedDisk.Speed {
				validationMessages = append(validationMessages, fmt.Sprintf(
					"disk '%s' on SCSI controller '%s' (bus %d) with SCSI unit ID %d has speed '%s' (expected '%s').",
					actualDisk.ID,
					actualSCSIController.ID,
					actualSCSIController.BusNumber,
					actualDisk.SCSIUnitID,
					actualDisk.Speed,
					expectedDisk.Speed,
				))
			}
		}

		for expectedSCSIPath := range expectedDisksBySCSIPath {
			expectedDisk := expectedDisksBySCSIPath[expectedSCSIPath]

			validationMessages = append(validationMessages, fmt.Sprintf(
				"no server disk was found on SCSI controller '%s' (bus %d) with SCSI unit ID %d.",
				actualSCSIController.ID,
				expectedDisk.SCSIBusNumber,
				expectedDisk.SCSIUnitID,
			))
		}

		if len(validationMessages) > 0 {
			return fmt.Errorf("bad: %s", strings.Join(validationMessages, ", "))
		}

		return nil
	}
}

// Acceptance test resource-destruction check for ddcloud_storage_controller:
//
// Check all servers specified in the configuration have been destroyed.
func testCheckDDCloudStorageControllerDestroy(state *terraform.State) error {
	for _, res := range state.RootModule().Resources {
		if res.Type != "ddcloud_storage_controller" {
			continue
		}

		controllerID := res.Primary.ID
		serverID := res.Primary.Attributes[resourceKeyStorageControllerServerID]

		client := testAccProvider.Meta().(*providerState).Client()
		server, err := client.GetServer(serverID)
		if err != nil {
			return fmt.Errorf("bad: get server '%s': %s", serverID, err)
		}
		if server == nil {
			return nil
		}

		storageController := server.SCSIControllers.GetByID(controllerID)
		if storageController != nil {
			return fmt.Errorf("storage controller '%s' still exists in server '%s'", controllerID, serverID)
		}
	}

	return nil
}
