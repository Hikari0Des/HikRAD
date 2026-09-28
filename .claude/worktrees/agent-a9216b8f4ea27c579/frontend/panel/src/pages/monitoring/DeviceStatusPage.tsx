import { useParams } from 'react-router-dom'

import { getDeviceProbes, listDevices } from '../../api/monitoring'
import { useAsync } from '../../hooks/useAsync'
import { StatusView } from './StatusView'

/** Per-device status page (FR-60): reuses the NAS status components, no RADIUS. */
export function DeviceStatusPage() {
  const { id = '' } = useParams()
  // No single-device endpoint exists; the device list is small (FR-60 caps
  // monitored devices well under one page), so resolve the name from it.
  const devices = useAsync(listDevices, [id])
  const device = devices.data?.items.find((d) => d.id === id)
  return (
    <StatusView
      title={device ? device.name : ''}
      subtitle={device?.ip}
      queryKey={id}
      fetcher={() => getDeviceProbes(id)}
    />
  )
}
