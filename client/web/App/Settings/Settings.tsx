import { ChevronRight, Globe, Users, Laptop, LockKeyhole } from 'lucide-react'
import PageHeader from '../PageHeader'
import Devices from './Devices'
import People from './People'
import Password from './Password'
import Website from './Website'

const pages = [
  { path: '/settings/site/', title: '网站', description: '网站标题', icon: Globe },
  { path: '/settings/people/', title: '人物', description: '平台账号归属与人物顺序', icon: Users },
  { path: '/settings/devices/', title: '设备', description: '在线设备与采集状态', icon: Laptop },
  {
    path: '/settings/password/',
    title: '修改密码',
    description: '更新登录密码',
    icon: LockKeyhole,
  },
]

export default function Settings({
  path,
  title,
  onTitle,
  onOwner,
  onLogin,
  onNavigate,
  onBack,
}: {
  path: string
  title: string
  onTitle: (title: string) => void
  onOwner: () => void
  onLogin: () => void
  onNavigate: (path: string) => void
  onBack: () => void
}) {
  const page = pages.find((item) => item.path === path)
  return (
    <>
      <PageHeader title={page?.title ?? '设置'} subpage onBack={onBack} />
      <section className="settings" aria-label={page?.title ?? '设置'}>
        {!page ? (
          <>
            <nav className="settings-menu" aria-label="设置分类">
              {pages.map(({ path, title, description, icon: Icon }) => (
                <a
                  key={path}
                  href={path}
                  className="settings-menu-item"
                  onClick={(event) => {
                    if (
                      event.button !== 0 ||
                      event.metaKey ||
                      event.ctrlKey ||
                      event.shiftKey ||
                      event.altKey
                    )
                      return
                    event.preventDefault()
                    onNavigate(path)
                  }}
                >
                  <Icon className="settings-menu-icon" aria-hidden="true" />
                  <span>
                    <strong>{title}</strong>
                    <small>{description}</small>
                  </span>
                  <ChevronRight size={20} aria-hidden="true" />
                </a>
              ))}
            </nav>
            <form className="settings-logout" method="post" action="/logout">
              <button type="submit">退出登录</button>
            </form>
          </>
        ) : (
          <div key={path}>
            {path === '/settings/site/' && (
              <Website title={title} onTitle={onTitle} onLogin={onLogin} />
            )}
            {path === '/settings/people/' && <People onLogin={onLogin} onOwner={onOwner} />}
            {path === '/settings/devices/' && <Devices onLogin={onLogin} />}
            {path === '/settings/password/' && <Password />}
          </div>
        )}
      </section>
    </>
  )
}
