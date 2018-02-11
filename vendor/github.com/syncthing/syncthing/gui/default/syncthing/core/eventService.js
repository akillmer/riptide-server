var debugEvents = !true;

angular.module('syncthing.core')
    .service('Events', ['$http', '$rootScope', '$timeout', function ($http, $rootScope, $timeout) {
        'use strict';

        var lastID = 0;
        var self = this;

        function successFn (data) {
            // When Syncthing restarts while the long polling connection is in
            // progress the browser on some platforms returns a 200 (since the
            // headers has been flushed with the return code 200), with no data.
            // This basically means that the connection has been reset, and the call
            // was not actually successful.
            if (!data) {
                errorFn(data);
                return;
            }
            $rootScope.$broadcast(self.ONLINE);

            if (lastID > 0) {   // not emit events from first response
                data.forEach(function (event) {
                    if (debugEvents) {
                        console.log("event", event.id, event.type, event.data);
                    }
                    $rootScope.$broadcast(event.type, event);
                });
            }

            var lastEvent = data.pop();
            if (lastEvent) {
                lastID = lastEvent.id;
            }

            $http.get(urlbase + '/events?since=' + lastID)
                .success(successFn)
                .error(errorFn);
        }

        function errorFn (dummy) {
            $rootScope.$broadcast(self.OFFLINE);

            $timeout(function () {
                $http.get(urlbase + '/events?limit=1')
                    .success(successFn)
                    .error(errorFn);
            }, 1000, false);
        }

        angular.extend(self, {
            // emitted by this

            ONLINE:  'UIOnline',
            OFFLINE: 'UIOffline',

            // emitted by syncthing process

            CONFIG_SAVED:         'ConfigSaved',   // Emitted after the config has been saved by the user or by Syncthing itself
            DEVICE_CONNECTED:     'DeviceConnected',   // Generated each time a connection to a device has been established
            DEVICE_DISCONNECTED:  'DeviceDisconnected',   // Generated each time a connection to a device has been terminated
            DEVICE_DISCOVERED:    'DeviceDiscovered',   // Emitted when a new device is discovered using local discovery
            DEVICE_REJECTED:      'DeviceRejected',   // Emitted when there is a connection from a device we are not configured to talk to
            DEVICE_PAUSED:        'DevicePaused',   // Emitted when a device has been paused
            DEVICE_RESUMED:       'DeviceResumed',   // Emitted when a device has been resumed
            DOWNLOAD_PROGRESS:    'DownloadProgress',   // Emitted during file downloads for each folder for each file
            FOLDER_COMPLETION:    'FolderCompletion',   //Emitted when the local or remote contents for a folder changes
            FOLDER_REJECTED:      'FolderRejected',   // Emitted when a device sends index information for a folder we do not have, or have but do not share with the device in question
            FOLDER_SUMMARY:       'FolderSummary',   // Emitted when folder contents have changed locally
            ITEM_FINISHED:        'ItemFinished',   // Generated when Syncthing ends synchronizing a file to a newer version
            ITEM_STARTED:         'ItemStarted',   // Generated when Syncthing begins synchronizing a file to a newer version
            LOCAL_INDEX_UPDATED:  'LocalIndexUpdated',   // Generated when the local index information has changed, due to synchronizing one or more items from the cluster or discovering local changes during a scan
            REMOTE_INDEX_UPDATED: 'RemoteIndexUpdated',   // Generated each time new index information is received from a device
            STARTING:             'Starting',   // Emitted exactly once, when Syncthing starts, before parsing configuration etc
            STARTUP_COMPLETED:    'StartupCompleted',   // Emitted exactly once, when initialization is complete and Syncthing is ready to start exchanging data with other devices
            STATE_CHANGED:        'StateChanged',   // Emitted when a folder changes state
            FOLDER_ERRORS:        'FolderErrors',   // Emitted when a folder has errors preventing a full sync
            FOLDER_SCAN_PROGRESS: 'FolderScanProgress',   // Emitted every ScanProgressIntervalS seconds, indicating how far into the scan it is at.
            FOLDER_PAUSED:        'FolderPaused',   // Emitted when a folder is paused
            FOLDER_RESUMED:       'FolderResumed',   // Emitted when a folder is resumed

            start: function() {
                $http.get(urlbase + '/events?limit=1')
                    .success(successFn)
                    .error(errorFn);
            }
        });
    }]);
