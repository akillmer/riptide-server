angular.module('syncthing.core')
    .directive('validDeviceid', function ($http) {
        return {
            require: 'ngModel',
            link: function (scope, elm, attrs, ctrl) {
                ctrl.$parsers.unshift(function (viewValue) {
                    if (scope.editingExisting) {
                        // we shouldn't validate
                        ctrl.$setValidity('validDeviceid', true);
                    } else {
                        $http.get(urlbase + '/svc/deviceid?id=' + viewValue).success(function (resp) {
                            if (resp.error) {
                                ctrl.$setValidity('validDeviceid', false);
                            } else {
                                ctrl.$setValidity('validDeviceid', true);
                            }
                        });
                        //Prevents user from adding a duplicate ID
                        var matches = scope.devices.filter(function (n) {
                            return n.deviceID == viewValue;
                        }).length;
                        if (matches > 0) {
                            ctrl.$setValidity('unique', false);
                        } else {
                            ctrl.$setValidity('unique', true);
                        }
                    }
                    return viewValue;
                });
            }
        };
});
