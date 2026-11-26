// The devices collectors run on, as they report themselves. What a collector collects, and when, is written in the collector on its device.
import { useCallback, useEffect, useState } from 'react'
import { fetchDevices, Unauthorized, type Device } from '../api'
import { subscribe } from '../events'

const when = (time?: string) => (time ? new Date(time).toLocaleString() : '')

export default function Devices({ onLogin }: { onLogin: () => void }) {
  const [devices, setDevices] = useState<Device[] | null>(null)
  const [failed, setFailed] = useState(false)

  const refresh = useCallback(async () => {
    try {
      setDevices((await fetchDevices()).filter((device) => device.online))
      setFailed(false)
    } catch (error) {
      if (error instanceof Unauthorized) onLogin()
      else setFailed(true)
    }
  }, [onLogin])
  useEffect(() => {
    void refresh()
  }, [refresh])
  useEffect(() => subscribe('collectors', () => void refresh(), 300, 2000), [refresh])
  // A device that stops reporting is not announced, so the list is read again now and then.
  useEffect(() => {
    const timer = window.setInterval(() => void refresh(), 60_000)
    return () => window.clearInterval(timer)
  }, [refresh])

  return (
    <div className="settings-section settings-section--list">
      <p className="settings-note">查看各设备的采集状态。</p>
      {failed && (
        <p className="settings-note" role="status">
          无法加载设备。
        </p>
      )}
      {devices && devices.length === 0 && !failed && (
        <p className="settings-note">暂无在线设备。</p>
      )}
      <ul className="people">
        {(devices ?? []).map((device) => (
          <li key={device.device} className="person">
            <div className="person-head">
              <strong className="person-name-text">{device.device}</strong>
              <span className="person-owner" data-online={true} data-tip={when(device.reportedAt)}>
                在线
              </span>
            </div>
            <ul className="owner-chips" aria-label="采集器">
              {device.collectors.map((collector) => (
                <li
                  key={collector.name}
                  title={[
                    collector.account,
                    collector.message,
                    collector.lastRun && `上次 ${when(collector.lastRun)}`,
                    collector.nextRun && `下次 ${when(collector.nextRun)}`,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                >
                  {collector.name}
                  {collector.state ? ` · ${collector.state}` : ''}
                </li>
              ))}
              {device.collectors.length === 0 && <li>没有采集器</li>}
            </ul>
          </li>
        ))}
      </ul>
    </div>
  )
}
