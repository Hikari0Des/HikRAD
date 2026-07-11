import { useParams } from 'react-router-dom'

import { getDeviceProbes } from '../../api/monitoring'
import { StatusView } from './StatusView'

/** Per-device status page (FR-60): reuses the NAS status components, no RADIUS. */
export function DeviceStatusPage() {
  const { id = '' } = useParams()
  return <StatusView title={id} fetcher={() => getDeviceProbes(id)} />
}
