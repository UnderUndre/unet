import './PrivilegeOverlay.css'

interface PrivilegeOverlayProps {
  onDismiss: () => void
}

function PrivilegeOverlay({ onDismiss }: PrivilegeOverlayProps) {
  return (
    <div className="privilege-overlay">
      <div className="privilege-card">
        <div className="privilege-icon">&#9888;</div>
        <h2 className="privilege-title">Privileged Access Required</h2>
        <p className="privilege-message">
          The Unet daemon is not running with administrator/root privileges.
          Many features — including tunnel creation, VPS provisioning, and port
          forwarding — require elevated permissions to function correctly.
        </p>
        <p className="privilege-hint">
          Please restart the daemon with <code>sudo</code> (Linux/macOS) or
          run as Administrator (Windows).
        </p>
        <button className="privilege-dismiss-btn" onClick={onDismiss}>
          I understand — continue anyway
        </button>
      </div>
    </div>
  )
}

export default PrivilegeOverlay
