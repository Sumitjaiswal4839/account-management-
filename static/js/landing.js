// AccSpace Landing Page Interactivity
document.addEventListener('DOMContentLoaded', () => {
    console.log("AccSpace Landing Page Loaded successfully.");

    // Smooth entry micro-animations or particle configurations can go here
    const ctaBtn = document.querySelector('.landing-cta');
    if (ctaBtn) {
        ctaBtn.addEventListener('mouseenter', () => {
            ctaBtn.style.transform = 'scale(1.03)';
        });
        ctaBtn.addEventListener('mouseleave', () => {
            ctaBtn.style.transform = 'scale(1)';
        });
    }
});
