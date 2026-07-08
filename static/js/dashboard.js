// AccSpace Dashboard Interaction Modules
document.addEventListener('DOMContentLoaded', () => {
    // DOM Elements
    const tableBody = document.getElementById('accounts-table-body');
    const totalAccountsEl = document.getElementById('stat-total-accounts');
    const roleFilter = document.getElementById('role-filter');
    const searchInput = document.getElementById('global-search');
    
    // Modal Elements
    const addModal = document.getElementById('add-account-modal');
    const addTrigger = document.getElementById('btn-add-account-trigger');
    const modalClose = document.getElementById('btn-modal-close');
    const modalCancel = document.getElementById('btn-modal-cancel');
    const addForm = document.getElementById('add-account-form');

    // Navigation Menu Tabs
    const navDashboard = document.getElementById('nav-dashboard');
    const navAccounts = document.getElementById('nav-accounts');
    const navSecurity = document.getElementById('nav-security');
    const navSettings = document.getElementById('nav-settings');

    // Panels
    const panelDashboard = document.getElementById('panel-dashboard');
    const panelAccounts = document.getElementById('panel-accounts');
    const panelSecurity = document.getElementById('panel-security');
    const panelSettings = document.getElementById('panel-settings');

    const menuItems = [navDashboard, navAccounts, navSecurity, navSettings];
    const panels = [panelDashboard, panelAccounts, panelSecurity, panelSettings];

    // SPA Tab Switching Logic
    function switchTab(targetNav, targetPanel) {
        // Toggle Active Classes on Sidebar Menu
        menuItems.forEach(item => {
            if (item) item.classList.remove('active');
        });
        if (targetNav) targetNav.classList.add('active');

        // Toggle Display on Panels
        panels.forEach(p => {
            if (p) p.style.display = 'none';
        });
        if (targetPanel) targetPanel.style.display = 'block';

        // Hide "Add Account" trigger button on settings/security views to keep UI clean
        if (targetNav === navSecurity || targetNav === navSettings) {
            if (addTrigger) addTrigger.style.display = 'none';
        } else {
            if (addTrigger) addTrigger.style.display = 'inline-flex';
        }
    }

    if (navDashboard) navDashboard.addEventListener('click', (e) => { e.preventDefault(); switchTab(navDashboard, panelDashboard); });
    if (navAccounts) navAccounts.addEventListener('click', (e) => { e.preventDefault(); switchTab(navAccounts, panelAccounts); });
    if (navSecurity) navSecurity.addEventListener('click', (e) => { e.preventDefault(); switchTab(navSecurity, panelSecurity); });
    if (navSettings) navSettings.addEventListener('click', (e) => { e.preventDefault(); switchTab(navSettings, panelSettings); });

    // Modal Control
    if (addTrigger) {
        addTrigger.addEventListener('click', () => addModal.classList.add('active'));
    }
    
    const closeModal = () => {
        addModal.classList.remove('active');
        addForm.reset();
    };

    if (modalClose) modalClose.addEventListener('click', closeModal);
    if (modalCancel) modalCancel.addEventListener('click', closeModal);

    // Client-side quick filter/search on the DOM rows
    function filterTable() {
        const filterVal = roleFilter ? roleFilter.value : 'all';
        const query = searchInput ? searchInput.value.toLowerCase() : '';
        if (!tableBody) return;
        const rows = tableBody.querySelectorAll('tr');
        let visibleCount = 0;

        rows.forEach(row => {
            if (row.classList.contains('no-match-row')) {
                row.remove();
                return;
            }

            const nameEl = row.querySelector('td:nth-child(1) span');
            const emailEl = row.querySelector('td:nth-child(2)');
            const roleEl = row.querySelector('td:nth-child(3) .badge');

            if (!nameEl || !emailEl || !roleEl) return;

            const name = nameEl.textContent.toLowerCase();
            const email = emailEl.textContent.toLowerCase();
            const role = roleEl.textContent.trim();

            const matchesRole = filterVal === 'all' || role === filterVal;
            const matchesSearch = name.includes(query) || email.includes(query);

            if (matchesRole && matchesSearch) {
                row.style.display = '';
                visibleCount++;
            } else {
                row.style.display = 'none';
            }
        });

        if (totalAccountsEl) {
            totalAccountsEl.textContent = visibleCount;
        }

        const activeRows = Array.from(rows).filter(r => r.style.display !== 'none');
        if (activeRows.length === 0 && rows.length > 0) {
            const noMatchRow = document.createElement('tr');
            noMatchRow.className = 'no-match-row';
            noMatchRow.innerHTML = `
                <td colspan="6" style="text-align: center; color: var(--text-secondary); padding: 32px;">
                    No accounts match your criteria.
                </td>
            `;
            tableBody.appendChild(noMatchRow);
        }
    }

    if (roleFilter) roleFilter.addEventListener('change', filterTable);
    if (searchInput) searchInput.addEventListener('input', filterTable);

    // Update count initial load
    if (totalAccountsEl && tableBody) {
        totalAccountsEl.textContent = tableBody.querySelectorAll('tr').length;
    }
});
