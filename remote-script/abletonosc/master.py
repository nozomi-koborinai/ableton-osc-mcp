"""Master track OSC handlers for AbletonOSC (mix-bus helpers)."""

from typing import Any, Tuple

from .handler import AbletonOSCHandler


class MasterHandler(AbletonOSCHandler):
    def __init__(self, manager):
        super().__init__(manager)
        self.class_identifier = "master"

    def init_api(self):
        def master_track():
            return self.song.master_track

        def get_volume(_params: Tuple[Any]):
            return (master_track().mixer_device.volume.value,)

        def set_volume(params: Tuple[Any]):
            master_track().mixer_device.volume.value = float(params[0])

        def get_meter_level(_params: Tuple[Any]):
            return (master_track().output_meter_level,)

        def get_meter_left(_params: Tuple[Any]):
            return (master_track().output_meter_left,)

        def get_meter_right(_params: Tuple[Any]):
            return (master_track().output_meter_right,)

        def get_num_devices(_params: Tuple[Any]):
            return (len(master_track().devices),)

        def get_devices_name(_params: Tuple[Any]):
            return tuple(d.name for d in master_track().devices)

        def get_devices_class_name(_params: Tuple[Any]):
            return tuple(d.class_name for d in master_track().devices)

        def get_devices_type(_params: Tuple[Any]):
            return tuple(int(d.type) for d in master_track().devices)

        def get_device_parameters_name(params: Tuple[Any]):
            device_index = int(params[0])
            device = master_track().devices[device_index]
            return tuple([device_index] + [p.name for p in device.parameters])

        def get_device_parameters_value(params: Tuple[Any]):
            device_index = int(params[0])
            device = master_track().devices[device_index]
            return tuple([device_index] + [p.value for p in device.parameters])

        def get_device_parameters_min(params: Tuple[Any]):
            device_index = int(params[0])
            device = master_track().devices[device_index]
            return tuple([device_index] + [p.min for p in device.parameters])

        def get_device_parameters_max(params: Tuple[Any]):
            device_index = int(params[0])
            device = master_track().devices[device_index]
            return tuple([device_index] + [p.max for p in device.parameters])

        def set_device_parameter(params: Tuple[Any]):
            device_index = int(params[0])
            parameter_index = int(params[1])
            value = float(params[2])
            master_track().devices[device_index].parameters[parameter_index].value = value
            return (device_index, parameter_index, value)

        self.osc_server.add_handler("/live/master/get/volume", get_volume)
        self.osc_server.add_handler("/live/master/set/volume", set_volume)
        self.osc_server.add_handler("/live/master/get/output_meter_level", get_meter_level)
        self.osc_server.add_handler("/live/master/get/output_meter_left", get_meter_left)
        self.osc_server.add_handler("/live/master/get/output_meter_right", get_meter_right)
        self.osc_server.add_handler("/live/master/get/num_devices", get_num_devices)
        self.osc_server.add_handler("/live/master/get/devices/name", get_devices_name)
        self.osc_server.add_handler("/live/master/get/devices/class_name", get_devices_class_name)
        self.osc_server.add_handler("/live/master/get/devices/type", get_devices_type)
        self.osc_server.add_handler("/live/master/device/get/parameters/name", get_device_parameters_name)
        self.osc_server.add_handler("/live/master/device/get/parameters/value", get_device_parameters_value)
        self.osc_server.add_handler("/live/master/device/get/parameters/min", get_device_parameters_min)
        self.osc_server.add_handler("/live/master/device/get/parameters/max", get_device_parameters_max)
        self.osc_server.add_handler("/live/master/device/set/parameter/value", set_device_parameter)
